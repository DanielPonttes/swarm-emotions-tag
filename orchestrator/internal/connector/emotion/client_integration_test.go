//go:build integration

package emotion_test

import (
	"context"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClientIntegration_AllRPCsAndInvalidArgument(t *testing.T) {
	addr := testutil.EnvOrDefault("EMOTION_ENGINE_ADDR", "127.0.0.1:50051")
	hostPort, err := testutil.ExtractHostPort(addr, "50051")
	if err != nil {
		t.Fatalf("extract emotion engine host: %v", err)
	}
	testutil.RequireTCP(t, hostPort)

	client, err := emotion.NewClient(addr)
	if err != nil {
		t.Fatalf("new emotion client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	agentID := testutil.UniqueID("it-emotion")
	now := time.Now().UnixMilli()

	if err := client.Ready(ctx); err != nil {
		t.Fatalf("ready check: %v", err)
	}

	transition, err := client.TransitionState(ctx, &connector.TransitionRequest{
		AgentID:      agentID,
		CurrentState: model.FsmState{StateName: "neutral", MacroState: "neutral", EnteredAt: now},
		Stimulus:     "novelty",
	})
	if err != nil {
		t.Fatalf("transition state: %v", err)
	}
	if !transition.TransitionOccurred {
		t.Fatalf("expected transition to occur")
	}
	if transition.NewState.StateName != "curious" {
		t.Fatalf("expected curious, got %q", transition.NewState.StateName)
	}

	compute, err := client.ComputeEmotionVector(ctx, &connector.ComputeRequest{
		CurrentEmotion: model.EmotionVector{Components: []float32{0.2, -0.4, 0.0, 0.5, 0.1, -0.2}},
		Trigger:        model.EmotionVector{Components: []float32{1.0, 0.5, -0.5, 0.0, 0.4, 2.0}},
		Baseline:       model.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
		WMatrix: []float32{
			0.1, 0, 0, 0, 0, 0,
			0, 0.1, 0, 0, 0, 0,
			0, 0, 0.1, 0, 0, 0,
			0, 0, 0, 0.1, 0, 0,
			0, 0, 0, 0, 0.1, 0,
			0, 0, 0, 0, 0, 0.1,
		},
		WDimension:  6,
		DecayLambda: 0,
		DeltaTime:   1,
	})
	if err != nil {
		t.Fatalf("compute emotion vector: %v", err)
	}
	if len(compute.NewEmotion.Components) != 6 {
		t.Fatalf("expected 6 emotion components, got %d", len(compute.NewEmotion.Components))
	}
	if compute.Intensity <= 0 {
		t.Fatalf("expected positive intensity, got %f", compute.Intensity)
	}

	fused, err := client.FuseScores(ctx, &connector.FuseRequest{
		Candidates: []model.ScoreCandidate{
			{MemoryID: "m1", SemanticScore: 0.4, EmotionalScore: 0.4, CognitiveScore: 0.4, MemoryLevel: 1},
			{MemoryID: "m2", SemanticScore: 0.3, EmotionalScore: 0.4, CognitiveScore: 0.5, MemoryLevel: 2, IsPseudopermanent: true},
		},
		Weights: model.ScoreWeights{
			Alpha:           0.4,
			Beta:            0.3,
			Gamma:           0.3,
			PseudopermBoost: 0.2,
		},
	})
	if err != nil {
		t.Fatalf("fuse scores: %v", err)
	}
	if len(fused.Ranked) != 2 {
		t.Fatalf("expected 2 ranked memories, got %d", len(fused.Ranked))
	}
	if fused.Ranked[0].MemoryID != "m2" {
		t.Fatalf("expected m2 ranked first, got %q", fused.Ranked[0].MemoryID)
	}

	promotion, err := client.EvaluatePromotion(ctx, &connector.PromotionRequest{
		Candidates: []model.PromotionCandidate{
			{MemoryID: "promo-1", Intensity: 0.95, CurrentLevel: 1, AccessFrequency: 1, ValenceMagnitude: 0.1},
		},
		IntensityThreshold: 0.9,
		FrequencyThreshold: 10,
		ValenceThreshold:   0.8,
	})
	if err != nil {
		t.Fatalf("evaluate promotion: %v", err)
	}
	if len(promotion.Decisions) != 1 {
		t.Fatalf("expected 1 promotion decision, got %d", len(promotion.Decisions))
	}
	if !promotion.Decisions[0].ShouldPromote || promotion.Decisions[0].TargetLevel != 2 {
		t.Fatalf("expected promotion to level 2, got %#v", promotion.Decisions[0])
	}

	cfg := model.DefaultAgentConfig(agentID)
	process, err := client.ProcessInteraction(ctx, &connector.ProcessRequest{
		AgentID:        agentID,
		CurrentState:   model.FsmState{StateName: "neutral", MacroState: "neutral", EnteredAt: now},
		CurrentEmotion: model.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
		Stimulus:       "praise",
		StimulusVector: model.EmotionVector{Components: []float32{0.5, 0.2, 0.1, 0, 0.1, 0}},
		Config:         cfg,
		ScoreCandidates: []model.ScoreCandidate{
			{MemoryID: "pm", SemanticScore: 0.2, EmotionalScore: 0.3, CognitiveScore: 0.4, MemoryLevel: 2, IsPseudopermanent: true},
		},
		PromotionCandidates: []model.PromotionCandidate{
			{MemoryID: "promo", Intensity: 0.95, CurrentLevel: 1, AccessFrequency: 12, ValenceMagnitude: 0.9},
		},
	})
	if err != nil {
		t.Fatalf("process interaction: %v", err)
	}
	if !process.TransitionOccurred {
		t.Fatalf("expected process interaction to transition state")
	}
	if process.NewState.StateName != "joyful" {
		t.Fatalf("expected joyful, got %q", process.NewState.StateName)
	}
	if len(process.Ranked) != 1 {
		t.Fatalf("expected 1 ranked memory, got %d", len(process.Ranked))
	}
	if len(process.Promotions) != 1 || !process.Promotions[0].ShouldPromote {
		t.Fatalf("expected promotion decision in process interaction, got %#v", process.Promotions)
	}

	_, err = client.ComputeEmotionVector(ctx, &connector.ComputeRequest{
		CurrentEmotion: model.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
		Trigger:        model.EmotionVector{Components: []float32{1, 0, 0, 0, 0, 0}},
		Baseline:       model.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
		WMatrix:        []float32{0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1, 0.1},
		WDimension:     3,
		DecayLambda:    0.1,
		DeltaTime:      1,
	})
	if err == nil {
		t.Fatalf("expected invalid argument error from compute emotion vector")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("expected invalid argument status, got %s (%v)", got, err)
	}
}
