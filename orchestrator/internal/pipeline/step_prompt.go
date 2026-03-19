package pipeline

import (
	"fmt"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func buildPrompt(
	input Input,
	ranked []model.RankedMemory,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
	workingMemory []model.WorkingMemoryEntry,
	systemPrompt string,
) string {
	if cognitive == nil {
		cognitive = model.DefaultCognitiveContext(input.AgentID)
	} else {
		cognitive = cognitive.Clone()
	}
	cognitive.Normalize()

	directive := FindEmotionDirective(fsmResult.NewEmotion)
	semanticMemories, resonantMemories := splitRankedMemories(ranked, fsmResult.NewIntensity)
	recentWorking := trimWorkingMemory(workingMemory, workingMemoryBudget(fsmResult.NewIntensity))

	var builder strings.Builder
	builder.Grow(1024)

	if base := strings.TrimSpace(systemPrompt); base != "" {
		builder.WriteString("Base system prompt:\n")
		builder.WriteString(base)
		builder.WriteString("\n\n")
	}

	builder.WriteString("## Agent Identity\n")
	builder.WriteString(fmt.Sprintf("AgentID: %s\n", input.AgentID))
	builder.WriteString("You are an emotionally coherent assistant operating inside EmotionRAG.\n\n")

	builder.WriteString("## Internal State\n")
	builder.WriteString(fmt.Sprintf("Directive: %s\n", directive.Directive))
	builder.WriteString(fmt.Sprintf("Tone hints: %s\n", strings.Join(directive.ToneHints, ", ")))
	builder.WriteString(fmt.Sprintf("FSM state: %s (%s)\n", fsmResult.NewFsmState.StateName, fsmResult.NewFsmState.MacroState))
	builder.WriteString(fmt.Sprintf("Conversation phase: %s\n", cognitive.ConversationPhase))
	builder.WriteString(fmt.Sprintf("Intensity: %.3f\n", fsmResult.NewIntensity))
	builder.WriteString(fmt.Sprintf("User expertise belief: %s\n", cognitive.Beliefs.UserExpertise))
	builder.WriteString(fmt.Sprintf("Task complexity belief: %s\n", cognitive.Beliefs.TaskComplexity))
	if cognitive.Beliefs.TimePressure {
		builder.WriteString("Time pressure: high, prioritize concise actionable guidance\n")
	}
	builder.WriteString("\n")

	builder.WriteString("## Behavioral Norms\n")
	builder.WriteString(fmt.Sprintf("Formality: %s\n", cognitive.Norms.FormalityLevel))
	builder.WriteString(fmt.Sprintf("Honesty: %s\n", cognitive.Norms.HonestyCommitment))
	builder.WriteString(fmt.Sprintf("Emotional expressiveness: %s\n", cognitive.Norms.EmotionalExpressiveness))
	builder.WriteString(fmt.Sprintf("Constraints: %s\n\n", joinOrFallback(cognitive.Constraints, "none")))

	builder.WriteString("## Active Goals\n")
	for _, goal := range cognitive.ActiveGoals {
		builder.WriteString(fmt.Sprintf("- %.2f %s\n", goal.Priority, goal.Description))
	}
	builder.WriteString("\n")

	builder.WriteString("## Semantic Context\n")
	if len(semanticMemories) == 0 {
		builder.WriteString("(no strong semantic documents)\n")
	} else {
		for _, memory := range semanticMemories {
			builder.WriteString(fmt.Sprintf("- %s\n", shortenText(memory.Content, 160)))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## Emotional Resonance\n")
	if len(resonantMemories) == 0 {
		builder.WriteString("(no resonant memories)\n")
	} else {
		for _, memory := range resonantMemories {
			builder.WriteString(fmt.Sprintf(
				"- [emotion %.2f] %s\n",
				memory.EmotionalContribution,
				shortenText(memory.Content, 160),
			))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## Working Memory\n")
	if summary := strings.TrimSpace(cognitive.WorkingSummary); summary != "" {
		builder.WriteString(fmt.Sprintf("Summary: %s\n", shortenText(summary, 180)))
	}
	if len(recentWorking) == 0 {
		builder.WriteString("(no recent working-memory entries)\n")
	} else {
		for _, entry := range recentWorking {
			builder.WriteString(fmt.Sprintf("- %s\n", shortenText(entry.Content, 120)))
		}
	}
	builder.WriteString("\n")

	builder.WriteString("## User Message\n")
	builder.WriteString(input.Text)
	builder.WriteString("\n\nRespond in the same language as the user when practical.")
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

func trimWorkingMemory(entries []model.WorkingMemoryEntry, budget int) []model.WorkingMemoryEntry {
	if budget <= 0 || len(entries) == 0 {
		return nil
	}
	if len(entries) <= budget {
		out := make([]model.WorkingMemoryEntry, len(entries))
		copy(out, entries)
		return out
	}
	out := make([]model.WorkingMemoryEntry, budget)
	copy(out, entries[len(entries)-budget:])
	return out
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
