package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

type PromptBudget struct {
	SystemTokens  int
	ContextTokens int
	MemoryTokens  int
	WorkingTokens int
	UserTokens    int
}

type PromptPackage struct {
	SystemPrompt string
	UserPrompt   string
	Directive    EmotionRegion
	Budget       PromptBudget
}

func buildPromptPackage(
	input Input,
	ranked []model.RankedMemory,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
	workingMemory []model.WorkingMemoryEntry,
	maxTokens int,
	baseSystemPrompt string,
) PromptPackage {
	if cognitive == nil {
		cognitive = model.DefaultCognitiveContext(input.AgentID)
	} else {
		cognitive = cognitive.Clone()
	}
	cognitive.Normalize()

	directive := FindEmotionDirective(fsmResult.NewEmotion)
	budget := calculatePromptBudget(maxTokens, fsmResult.NewIntensity)
	semanticMemories, resonantMemories := splitRankedMemories(ranked, fsmResult.NewIntensity)
	recentWorking := normalizeWorkingMemoryForPrompt(workingMemory, workingMemoryBudget(fsmResult.NewIntensity))

	systemPrompt := buildSystemPrompt(baseSystemPrompt, fsmResult, cognitive, directive, budget)
	userPrompt := buildUserPrompt(input, cognitive, semanticMemories, resonantMemories, recentWorking, budget)

	return PromptPackage{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Directive:    directive,
		Budget:       budget,
	}
}

func buildPrompt(
	input Input,
	ranked []model.RankedMemory,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
	workingMemory []model.WorkingMemoryEntry,
	systemPrompt string,
) string {
	return buildPromptPackage(
		input,
		ranked,
		fsmResult,
		cognitive,
		workingMemory,
		256,
		systemPrompt,
	).UserPrompt
}

func calculatePromptBudget(maxTokens int, intensity float32) PromptBudget {
	if maxTokens <= 0 {
		maxTokens = 256
	}

	systemRatio := 0.28
	contextRatio := 0.32
	memoryRatio := 0.18
	workingRatio := 0.12

	if intensity > 0.7 {
		memoryRatio += 0.10
		contextRatio -= 0.06
		workingRatio -= 0.04
	}

	system := int(float64(maxTokens) * systemRatio)
	context := int(float64(maxTokens) * contextRatio)
	memory := int(float64(maxTokens) * memoryRatio)
	working := int(float64(maxTokens) * workingRatio)
	user := maxTokens - system - context - memory - working

	return PromptBudget{
		SystemTokens:  maxInt(system, 72),
		ContextTokens: maxInt(context, 64),
		MemoryTokens:  maxInt(memory, 48),
		WorkingTokens: maxInt(working, 32),
		UserTokens:    maxInt(user, 32),
	}
}

func buildSystemPrompt(
	baseSystemPrompt string,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
	directive EmotionRegion,
	budget PromptBudget,
) string {
	var builder strings.Builder
	builder.Grow(approxChars(budget.SystemTokens))

	if base := strings.TrimSpace(baseSystemPrompt); base != "" {
		builder.WriteString(base)
		builder.WriteString("\n\n")
	}

	builder.WriteString("Operate as an emotionally coherent assistant inside EmotionRAG.\n")
	builder.WriteString(fmt.Sprintf("Emotional directive: %s\n", directive.Directive))
	builder.WriteString(fmt.Sprintf("Tone hints: %s\n", strings.Join(directive.ToneHints, ", ")))
	builder.WriteString(fmt.Sprintf("FSM state: %s (%s)\n", fsmResult.NewFsmState.StateName, fsmResult.NewFsmState.MacroState))
	builder.WriteString(fmt.Sprintf("Conversation phase: %s\n", cognitive.ConversationPhase))
	builder.WriteString(fmt.Sprintf("Emotion intensity: %.3f\n", fsmResult.NewIntensity))
	builder.WriteString(fmt.Sprintf(
		"Norms: formality=%s; honesty=%s; expressiveness=%s\n",
		cognitive.Norms.FormalityLevel,
		cognitive.Norms.HonestyCommitment,
		cognitive.Norms.EmotionalExpressiveness,
	))
	builder.WriteString(fmt.Sprintf("Constraints: %s\n", joinOrFallback(limitStrings(cognitive.Constraints, 3), "none")))
	builder.WriteString(fmt.Sprintf("Active goals: %s\n", joinOrFallback(goalDescriptions(cognitive.ActiveGoals, 3), "resolve the request")))
	if cognitive.Beliefs.TimePressure {
		builder.WriteString("The user appears time-constrained. Prefer direct next steps.\n")
	}
	builder.WriteString("Respond in the same language as the user when practical.")

	return strings.TrimSpace(builder.String())
}

func buildUserPrompt(
	input Input,
	cognitive *model.CognitiveContext,
	semanticMemories []model.RankedMemory,
	resonantMemories []model.RankedMemory,
	workingMemory []model.WorkingMemoryEntry,
	budget PromptBudget,
) string {
	contextChars := approxChars(budget.ContextTokens)
	memoryChars := approxChars(budget.MemoryTokens)
	workingChars := approxChars(budget.WorkingTokens)
	userChars := approxChars(budget.UserTokens)

	var builder strings.Builder
	builder.Grow(contextChars + memoryChars + workingChars + userChars + 256)

	builder.WriteString("## Semantic Context\n")
	if len(semanticMemories) == 0 {
		builder.WriteString("(no strong semantic documents)\n")
	} else {
		perItem := maxInt(contextChars/maxInt(len(semanticMemories), 1), 64)
		for _, memory := range semanticMemories {
			builder.WriteString(fmt.Sprintf("- %s\n", shortenText(memory.Content, perItem)))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## Emotional Resonance\n")
	if len(resonantMemories) == 0 {
		builder.WriteString("(no resonant memories)\n")
	} else {
		perItem := maxInt(memoryChars/maxInt(len(resonantMemories), 1), 64)
		for _, memory := range resonantMemories {
			builder.WriteString(fmt.Sprintf(
				"- [emotion %.2f] %s\n",
				memory.EmotionalContribution,
				shortenText(memory.Content, perItem),
			))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## Working Memory\n")
	if summary := strings.TrimSpace(cognitive.WorkingSummary); summary != "" {
		builder.WriteString(fmt.Sprintf("Summary: %s\n", shortenText(summary, workingChars)))
	}
	if len(workingMemory) == 0 {
		builder.WriteString("(no recent working-memory entries)\n")
	} else {
		perItem := maxInt(workingChars/maxInt(len(workingMemory), 1), 48)
		for _, entry := range workingMemory {
			role := normalizeWorkingMemoryRole(entry.Role)
			if role != "" {
				builder.WriteString(fmt.Sprintf("%s: %s\n", role, shortenText(entry.Content, perItem)))
				continue
			}
			builder.WriteString(fmt.Sprintf("- %s\n", shortenText(entry.Content, perItem)))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## User Message\n")
	builder.WriteString(shortenText(input.Text, userChars))
	return builder.String()
}

func splitRankedMemories(
	ranked []model.RankedMemory,
	intensity float32,
) ([]model.RankedMemory, []model.RankedMemory) {
	semanticBudget := 2
	resonantBudget := 2
	if intensity > 0.7 {
		semanticBudget = 1
		resonantBudget = 3
	}

	semantic := make([]model.RankedMemory, 0, semanticBudget)
	resonant := make([]model.RankedMemory, 0, resonantBudget)
	fallback := make([]model.RankedMemory, 0, len(ranked))

	for _, item := range ranked {
		if item.EmotionalContribution >= item.SemanticContribution && len(resonant) < resonantBudget {
			resonant = append(resonant, item)
			continue
		}
		if item.SemanticContribution > item.EmotionalContribution && len(semantic) < semanticBudget {
			semantic = append(semantic, item)
			continue
		}
		fallback = append(fallback, item)
	}

	for _, item := range fallback {
		if len(semantic) < semanticBudget {
			semantic = append(semantic, item)
			continue
		}
		if len(resonant) < resonantBudget {
			resonant = append(resonant, item)
		}
	}
	return semantic, resonant
}

func workingMemoryBudget(intensity float32) int {
	if intensity > 0.8 {
		return 2
	}
	return 3
}

func normalizeWorkingMemoryForPrompt(entries []model.WorkingMemoryEntry, budget int) []model.WorkingMemoryEntry {
	if budget <= 0 || len(entries) == 0 {
		return nil
	}

	ordered := make([]model.WorkingMemoryEntry, len(entries))
	copy(ordered, entries)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].CreatedAtMs > ordered[j].CreatedAtMs
	})
	if len(ordered) > budget {
		ordered = ordered[:budget]
	}
	out := make([]model.WorkingMemoryEntry, len(ordered))
	for i := range ordered {
		out[len(ordered)-1-i] = ordered[i]
	}
	return out
}

func normalizeWorkingMemoryRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "User"
	case "assistant", "agent":
		return "Assistant"
	default:
		return ""
	}
}

func joinOrFallback(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, ", ")
}

func shortenText(text string, limit int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func goalDescriptions(goals []model.CognitiveGoal, limit int) []string {
	if len(goals) == 0 {
		return nil
	}
	if limit <= 0 || len(goals) <= limit {
		out := make([]string, 0, len(goals))
		for _, goal := range goals {
			out = append(out, goal.Description)
		}
		return out
	}
	out := make([]string, 0, limit)
	for _, goal := range goals[:limit] {
		out = append(out, goal.Description)
	}
	return out
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func approxChars(tokens int) int {
	return maxInt(tokens*4, 48)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
