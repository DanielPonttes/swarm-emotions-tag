package pipeline

import (
	"context"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func (o *Orchestrator) stepPostProcess(
	ctx context.Context,
	input Input,
	llmResponse string,
	fsmResult *FSMResult,
	ranked []model.RankedMemory,
) {
	now := time.Now().UnixMilli()

	promotionCandidates := make([]model.PromotionCandidate, 0, len(ranked))
	for _, item := range ranked {
		promotionCandidates = append(promotionCandidates, model.PromotionCandidate{
			MemoryID:         item.MemoryID,
			Intensity:        fsmResult.NewIntensity,
			CurrentLevel:     1,
			AccessFrequency:  1,
			ValenceMagnitude: absFloat32(firstComponent(fsmResult.NewEmotion)),
		})
	}

	_, _ = o.emotionClient.EvaluatePromotion(ctx, &connector.PromotionRequest{
		Candidates:         promotionCandidates,
		IntensityThreshold: 0.9,
		FrequencyThreshold: 10,
		ValenceThreshold:   0.8,
	})

	_ = o.db.LogInteraction(ctx, &model.InteractionLog{
		AgentID:      input.AgentID,
		UserText:     input.Text,
		ResponseText: llmResponse,
		Stimulus:     fsmResult.Stimulus,
		FsmState:     fsmResult.NewFsmState,
		Emotion:      fsmResult.NewEmotion,
		Intensity:    fsmResult.NewIntensity,
		CreatedAtMs:  now,
	})

	_ = o.db.AppendEmotionHistory(ctx, &model.EmotionHistoryEntry{
		AgentID:     input.AgentID,
		FsmState:    fsmResult.NewFsmState,
		Emotion:     fsmResult.NewEmotion,
		Intensity:   fsmResult.NewIntensity,
		CreatedAtMs: now,
	})

	summary := "No cognitive notes"
	if len(ranked) > 0 {
		summary = ranked[0].Content
		_ = o.cache.PushWorkingMemory(ctx, input.AgentID, model.WorkingMemoryEntry{
			MemoryID:    ranked[0].MemoryID,
			Content:     ranked[0].Content,
			Score:       ranked[0].FinalScore,
			CreatedAtMs: now,
		})
	}

	_ = o.db.UpdateCognitiveContext(ctx, input.AgentID, &model.CognitiveContext{
		AgentID:        input.AgentID,
		Goals:          []string{"Respond helpfully", "Preserve emotional continuity"},
		Constraints:    []string{"Stay concise", "Do not invent facts"},
		WorkingSummary: summary,
		UpdatedAtMs:    now,
	})
}

func firstComponent(v model.EmotionVector) float32 {
	if len(v.Components) == 0 {
		return 0
	}
	return v.Components[0]
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
