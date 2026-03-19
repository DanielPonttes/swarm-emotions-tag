package pipeline

import (
	"context"
	"log/slog"
	"strings"
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
	cognitive *model.CognitiveContext,
	directive EmotionRegion,
) {
	now := time.Now().UnixMilli()
	responseEmotion, err := o.classifier.ClassifyEmotion(ctx, llmResponse)
	if err != nil {
		slog.Warn("failed to classify response emotion", "error", err)
	} else {
		measureToneCompliance(directive, responseEmotion, o.metrics)
	}

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

	summary := buildWorkingSummary(input.Text, llmResponse, ranked)
	_ = o.cache.PushWorkingMemory(ctx, input.AgentID, model.WorkingMemoryEntry{
		MemoryID:    "turn-user",
		Role:        "user",
		Content:     input.Text,
		Score:       fsmResult.NewIntensity,
		CreatedAtMs: now,
	})
	_ = o.cache.PushWorkingMemory(ctx, input.AgentID, model.WorkingMemoryEntry{
		MemoryID:    "turn-assistant",
		Role:        "assistant",
		Content:     llmResponse,
		Score:       fsmResult.NewIntensity,
		CreatedAtMs: now + 1,
	})

	nextContext := model.DefaultCognitiveContext(input.AgentID)
	if cognitive != nil {
		nextContext = cognitive.Clone()
		nextContext.AgentID = input.AgentID
	}
	nextContext.WorkingSummary = summary
	nextContext.Beliefs.WorkingSummary = summary
	nextContext.UpdatedAtMs = now
	nextContext.Normalize()

	_ = o.db.UpdateCognitiveContext(ctx, input.AgentID, nextContext)
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

func buildWorkingSummary(inputText, llmResponse string, ranked []model.RankedMemory) string {
	parts := make([]string, 0, 3)
	if trimmed := shortenText(inputText, 80); trimmed != "" {
		parts = append(parts, "user="+trimmed)
	}
	if len(ranked) > 0 {
		parts = append(parts, "memory="+shortenText(ranked[0].Content, 80))
	}
	if trimmed := shortenText(llmResponse, 80); trimmed != "" {
		parts = append(parts, "response="+trimmed)
	}
	if len(parts) == 0 {
		return "No cognitive notes"
	}
	return strings.Join(parts, " | ")
}
