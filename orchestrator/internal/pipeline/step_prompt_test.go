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
	promptPackage := buildPromptPackage(
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
		256,
		"You are the base system prompt.",
	)

	for _, expected := range []string{
		"Emotional directive:",
		"Tone hints:",
		"Conversation phase:",
		"Respond in the same language as the user",
	} {
		if !strings.Contains(promptPackage.SystemPrompt, expected) {
			t.Fatalf("expected system prompt to contain %q\n%s", expected, promptPackage.SystemPrompt)
		}
	}
	for _, expected := range []string{"## Emotional Resonance", "## Working Memory", "## User Message"} {
		if !strings.Contains(promptPackage.UserPrompt, expected) {
			t.Fatalf("expected user prompt to contain %q\n%s", expected, promptPackage.UserPrompt)
		}
	}
}

func TestCalculatePromptBudgetAllocatesMoreMemoryAtHighIntensity(t *testing.T) {
	low := calculatePromptBudget(256, 0.2)
	high := calculatePromptBudget(256, 0.9)

	if high.MemoryTokens <= low.MemoryTokens {
		t.Fatalf("expected high intensity to increase memory budget: low=%+v high=%+v", low, high)
	}
	if high.ContextTokens >= low.ContextTokens {
		t.Fatalf("expected high intensity to reduce context budget: low=%+v high=%+v", low, high)
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

func TestToneComplianceScoreHighWhenAlignedAndLowWhenMisaligned(t *testing.T) {
	aligned := toneComplianceScore(
		[3]float32{-0.7, 0.9, -0.4},
		[3]float32{-0.65, 0.85, -0.35},
	)
	misaligned := toneComplianceScore(
		[3]float32{-0.7, 0.9, -0.4},
		[3]float32{0.7, -0.5, 0.6},
	)

	if aligned <= misaligned {
		t.Fatalf("expected aligned score > misaligned score: aligned=%f misaligned=%f", aligned, misaligned)
	}
	if aligned < 0.8 {
		t.Fatalf("expected high aligned score, got %f", aligned)
	}
	if misaligned > 0.4 {
		t.Fatalf("expected low misaligned score, got %f", misaligned)
	}
}
