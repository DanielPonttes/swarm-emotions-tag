package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func prepareCognitiveContext(
	base *model.CognitiveContext,
	input string,
	emotion model.EmotionVector,
) *model.CognitiveContext {
	if base == nil {
		base = model.DefaultCognitiveContext("")
	} else {
		base = base.Clone()
	}
	base.Normalize()

	base.ConversationPhase = inferConversationPhase(base.ConversationPhase, input, emotion)
	base.Beliefs = updateBeliefs(base.Beliefs, input, emotion)
	base.ActiveGoals = reprioritizeGoals(base.ActiveGoals, base.ConversationPhase, base.Beliefs)
	base.Normalize()
	return base
}

func inferConversationPhase(current, input string, emotion model.EmotionVector) string {
	lower := strings.ToLower(input)
	switch {
	case containsAny(lower, "error", "bug", "broken", "help", "problema", "erro", "falhou", "issue", "deadline", "urgent", "urgente"):
		return "problem_diagnosis"
	case containsAny(lower, "resolved", "fix", "fixed", "works", "working", "funcionou", "resolveu"):
		return "resolution"
	case current == "" || current == "idle":
		if containsAny(lower, "bye", "goodbye", "tchau", "obrigado", "thanks") {
			return "farewell"
		}
		return "greeting"
	case containsAny(lower, "bye", "goodbye", "tchau", "obrigado", "thanks"):
		return "farewell"
	case emotion.Intensity() < 0.45 && current == "problem_diagnosis":
		return "resolution"
	default:
		return current
	}
}

func updateBeliefs(
	beliefs model.CognitiveBeliefs,
	input string,
	emotion model.EmotionVector,
) model.CognitiveBeliefs {
	lower := strings.ToLower(input)
	technicalHits := countTechnicalTerms(lower)

	switch {
	case technicalHits >= 3:
		beliefs.UserExpertise = "advanced"
	case technicalHits >= 1:
		beliefs.UserExpertise = "intermediate"
	case beliefs.UserExpertise == "" || beliefs.UserExpertise == "unknown":
		beliefs.UserExpertise = "beginner"
	}

	switch {
	case technicalHits >= 4 || len(strings.Fields(lower)) > 24:
		beliefs.TaskComplexity = "high"
	case technicalHits >= 2 || len(strings.Fields(lower)) > 10:
		beliefs.TaskComplexity = "medium"
	default:
		beliefs.TaskComplexity = "low"
	}

	arousal := emotionComponent(emotion, 1)
	valence := emotionComponent(emotion, 0)
	beliefs.TimePressure = beliefs.TimePressure ||
		containsAny(lower, "urgent", "urgente", "asap", "today", "hoje", "deadline", "agora") ||
		(arousal > 0.55 && valence < 0)

	beliefs.UserEmotionalEstimate = append([]float32(nil), emotion.Components...)
	return beliefs
}

func reprioritizeGoals(
	current []model.CognitiveGoal,
	phase string,
	beliefs model.CognitiveBeliefs,
) []model.CognitiveGoal {
	goalMap := make(map[string]model.CognitiveGoal)
	for _, goal := range current {
		if goal.ID == "" {
			goal.ID = slugGoal(goal.Description)
		}
		goalMap[goal.ID] = goal
	}

	upsertGoal(goalMap, model.CognitiveGoal{
		ID:          "resolve_request",
		Description: "Resolve the user's request",
		Priority:    0.9,
	})
	upsertGoal(goalMap, model.CognitiveGoal{
		ID:          "emotional_continuity",
		Description: "Preserve emotional continuity",
		Priority:    0.7,
	})

	if beliefs.TimePressure {
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "reduce_friction",
			Description: "Minimize friction and get to the action quickly",
			Priority:    1.0,
		})
	}
	if beliefs.UserExpertise == "beginner" {
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "plain_language",
			Description: "Explain the next step in plain language",
			Priority:    0.92,
		})
	}
	if beliefs.UserExpertise == "advanced" {
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "technical_precision",
			Description: "Provide technically precise guidance",
			Priority:    0.88,
		})
	}

	switch phase {
	case "greeting":
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "establish_context",
			Description: "Establish context before diving into details",
			Priority:    0.72,
		})
	case "problem_diagnosis":
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "diagnose_problem",
			Description: "Identify the most likely root cause",
			Priority:    0.96,
		})
	case "resolution":
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "close_loop",
			Description: "Confirm resolution and next steps",
			Priority:    0.82,
		})
	case "farewell":
		upsertGoal(goalMap, model.CognitiveGoal{
			ID:          "clean_handoff",
			Description: "Close the interaction cleanly",
			Priority:    0.76,
		})
	}

	goals := make([]model.CognitiveGoal, 0, len(goalMap))
	for _, goal := range goalMap {
		goals = append(goals, goal)
	}
	sort.Slice(goals, func(i, j int) bool {
		if goals[i].Priority == goals[j].Priority {
			return goals[i].Description < goals[j].Description
		}
		return goals[i].Priority > goals[j].Priority
	})
	if len(goals) > 4 {
		goals = goals[:4]
	}
	return goals
}

func applyCognitiveReranking(
	candidates []model.ScoreCandidate,
	cognitive *model.CognitiveContext,
	input string,
) {
	if cognitive == nil {
		return
	}
	lowerInput := strings.ToLower(input)
	for i := range candidates {
		score := candidates[i].CognitiveScore
		content := strings.ToLower(candidates[i].Content)

		if cognitive.Beliefs.TimePressure && wordCount(content) <= 8 {
			score += 0.15
		}
		if cognitive.Beliefs.UserExpertise == "beginner" && isHighlyTechnical(content) {
			score -= 0.18
		}
		if cognitive.Beliefs.UserExpertise == "advanced" && isHighlyTechnical(content) {
			score += 0.10
		}
		if cognitive.ConversationPhase == "problem_diagnosis" &&
			containsAny(content, "issue", "problem", "error", "troubleshoot", "failure", "bug") {
			score += 0.12
		}
		if cognitive.ConversationPhase == "resolution" &&
			containsAny(content, "preference", "next step", "resolved", "concise", "summary") {
			score += 0.08
		}
		for _, goal := range cognitive.ActiveGoals {
			if goalMatchesCandidate(goal, content, lowerInput) {
				score += 0.08 * goal.Priority
			}
		}

		candidates[i].CognitiveScore = clampUnit(score)
	}
}

func upsertGoal(goalMap map[string]model.CognitiveGoal, goal model.CognitiveGoal) {
	if existing, ok := goalMap[goal.ID]; ok {
		if goal.Priority > existing.Priority {
			existing.Priority = goal.Priority
		}
		if existing.Description == "" {
			existing.Description = goal.Description
		}
		goalMap[goal.ID] = existing
		return
	}
	goalMap[goal.ID] = goal
}

func goalMatchesCandidate(goal model.CognitiveGoal, content, input string) bool {
	description := strings.ToLower(goal.Description)
	goalKeywords := strings.Fields(description)
	for _, keyword := range goalKeywords {
		if len(keyword) < 5 {
			continue
		}
		if strings.Contains(content, keyword) {
			return true
		}
	}

	return (strings.Contains(description, "concise") && strings.Contains(content, "concise")) ||
		(strings.Contains(description, "root cause") && containsAny(content, "issue", "problem", "error", "bug")) ||
		(strings.Contains(description, "plain language") && !isHighlyTechnical(content) && !containsAny(input, "grpc", "protobuf"))
}

func slugGoal(goal string) string {
	goal = strings.ToLower(goal)
	replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_")
	goal = replacer.Replace(goal)
	return fmt.Sprintf("goal_%s", goal)
}

func countTechnicalTerms(input string) int {
	terms := []string{
		"grpc", "protobuf", "qdrant", "redis", "postgres", "docker", "ollama",
		"latency", "throughput", "goroutine", "vector", "cache", "schema",
		"endpoint", "json", "payload", "timeout", "rebase", "merge", "commit",
	}
	count := 0
	for _, term := range terms {
		if strings.Contains(input, term) {
			count++
		}
	}
	return count
}

func isHighlyTechnical(text string) bool {
	return countTechnicalTerms(text) >= 2 ||
		containsAny(text, "function", "implementation", "connector", "integration", "pipeline")
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func emotionComponent(emotion model.EmotionVector, idx int) float32 {
	if idx < 0 || idx >= len(emotion.Components) {
		return 0
	}
	return emotion.Components[idx]
}

func clampUnit(value float32) float32 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func wordCount(text string) int {
	return len(strings.Fields(strings.TrimSpace(text)))
}
