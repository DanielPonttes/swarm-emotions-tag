package pipeline

import (
	"strings"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestPrepareCognitiveContextInfersPhaseAndBeliefs(t *testing.T) {
	ctx := prepareCognitiveContext(
		model.DefaultCognitiveContext("agent-1"),
		"urgent bug on redis cache, need a fix today",
		model.EmotionVector{Components: []float32{-0.4, 0.8, 0.2, 0, 0, 0}},
	)

	if ctx.ConversationPhase != "problem_diagnosis" {
		t.Fatalf("expected problem_diagnosis, got %q", ctx.ConversationPhase)
	}
	if !ctx.Beliefs.TimePressure {
		t.Fatalf("expected time pressure to be true")
	}
	if ctx.Beliefs.UserExpertise == "beginner" {
		t.Fatalf("expected technical input to raise expertise belief, got %q", ctx.Beliefs.UserExpertise)
	}
	if len(ctx.ActiveGoals) == 0 || ctx.ActiveGoals[0].Description == "" {
		t.Fatalf("expected active goals to be reprioritized")
	}
}

func TestApplyCognitiveRerankingPenalizesTechnicalContentForBeginners(t *testing.T) {
	candidates := []model.ScoreCandidate{
		{MemoryID: "simple", Content: "concise next step summary", CognitiveScore: 0.2},
		{MemoryID: "technical", Content: "grpc protobuf connector schema implementation details", CognitiveScore: 0.2},
	}
	cognitive := model.DefaultCognitiveContext("agent-1")
	cognitive.Beliefs.UserExpertise = "beginner"
	cognitive.Beliefs.TimePressure = true
	cognitive.ConversationPhase = "problem_diagnosis"

	applyCognitiveReranking(candidates, cognitive, "urgent help")

	if candidates[0].CognitiveScore <= candidates[1].CognitiveScore {
		t.Fatalf("expected simple memory to outrank technical one cognitively: %#v", candidates)
	}
}

func TestBuildPromptIncludesStructuredCognitiveSections(t *testing.T) {
	prompt := buildPrompt(
		Input{AgentID: "agent-9", Text: "Preciso corrigir o push agora"},
		[]model.RankedMemory{
			{
				MemoryID:              "m1",
				Content:               "Semantic document about git remote divergence",
				SemanticContribution:  0.5,
				EmotionalContribution: 0.1,
			},
			{
				MemoryID:              "m2",
				Content:               "Resonant memory about concise incident response",
				SemanticContribution:  0.2,
				EmotionalContribution: 0.6,
			},
		},
		&FSMResult{
			NewEmotion:   model.EmotionVector{Components: []float32{-0.6, 0.9, -0.3, 0, 0, 0}},
			NewFsmState:  model.FsmState{StateName: "anxious", MacroState: "negative"},
			NewIntensity: 0.92,
		},
		prepareCognitiveContext(model.DefaultCognitiveContext("agent-9"), "push urgente", model.EmotionVector{Components: []float32{-0.6, 0.9, -0.3}}),
		[]model.WorkingMemoryEntry{{Content: "Última tentativa de deploy falhou por rejeição no remote."}},
		"You are the base system prompt.",
	)

	for _, expected := range []string{
		"## Internal State",
		"## Emotional Resonance",
		"Conversation phase:",
		"Time pressure: high",
		"Respond in the same language as the user",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q\n%s", expected, prompt)
		}
	}
}

func TestFindEmotionDirectiveMatchesPanicAndNeutralFallback(t *testing.T) {
	panicDirective := FindEmotionDirective(model.EmotionVector{Components: []float32{-0.7, 0.9, -0.4}})
	if panicDirective.Name != "panic" {
		t.Fatalf("expected panic directive, got %q", panicDirective.Name)
	}

	neutralDirective := FindEmotionDirective(model.EmotionVector{Components: []float32{0, 0, 0}})
	if neutralDirective.Name == "" {
		t.Fatalf("expected fallback directive")
	}
}
