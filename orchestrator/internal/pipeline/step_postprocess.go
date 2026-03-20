package pipeline

import (
	"context"
	"fmt"
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
	persistPromotedInteractionMemory(ctx, o, input, llmResponse, fsmResult, ranked, now)

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

func persistPromotedInteractionMemory(
	ctx context.Context,
	orchestrator *Orchestrator,
	input Input,
	llmResponse string,
	fsmResult *FSMResult,
	ranked []model.RankedMemory,
	now int64,
) {
	memoryID := fmt.Sprintf("%s-turn-%d", input.AgentID, now)
	promotion, err := orchestrator.emotionClient.EvaluatePromotion(ctx, &connector.PromotionRequest{
		Candidates: []model.PromotionCandidate{
			{
				MemoryID:         memoryID,
				Intensity:        fsmResult.NewIntensity,
				CurrentLevel:     1,
				AccessFrequency:  1,
				ValenceMagnitude: absFloat32(firstComponent(fsmResult.NewEmotion)),
			},
		},
		IntensityThreshold: 0.9,
		FrequencyThreshold: 10,
		ValenceThreshold:   0.8,
	})
	if err != nil || promotion == nil || len(promotion.Decisions) == 0 {
		return
	}

	decision := promotion.Decisions[0]
	if !decision.ShouldPromote || decision.TargetLevel < 2 {
		return
	}

	_ = orchestrator.vectorStore.UpsertMemory(ctx, model.StoredMemory{
		MemoryID:          memoryID,
		AgentID:           input.AgentID,
		Content:           buildInteractionMemoryContent(input.Text, llmResponse),
		Emotion:           fsmResult.NewEmotion,
		Intensity:         fsmResult.NewIntensity,
		CognitiveScore:    promotedMemoryCognitiveScore(ranked, fsmResult.NewIntensity),
		MemoryLevel:       decision.TargetLevel,
		IsPseudopermanent: decision.TargetLevel >= 3,
		CreatedAtMs:       now,
	})
}

func buildInteractionMemoryContent(inputText, llmResponse string) string {
	parts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(inputText); trimmed != "" {
		parts = append(parts, "User: "+trimmed)
	}
	if trimmed := strings.TrimSpace(llmResponse); trimmed != "" {
		parts = append(parts, "Assistant: "+trimmed)
	}
	return strings.Join(parts, "\n")
}

func promotedMemoryCognitiveScore(ranked []model.RankedMemory, intensity float32) float32 {
	score := intensity
	if len(ranked) > 0 && ranked[0].FinalScore > score {
		score = ranked[0].FinalScore
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
