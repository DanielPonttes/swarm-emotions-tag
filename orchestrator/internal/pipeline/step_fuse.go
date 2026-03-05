package pipeline

import (
	"context"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func (o *Orchestrator) stepFuse(
	ctx context.Context,
	candidates []model.ScoreCandidate,
	config *model.AgentConfig,
	currentEmotion model.EmotionVector,
) ([]model.RankedMemory, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.15, 250000000)
	defer cancel()

	response, err := o.emotionClient.FuseScores(stepCtx, &connector.FuseRequest{
		Candidates:     candidates,
		Weights:        config.Weights,
		CurrentEmotion: currentEmotion,
	})
	if err != nil {
		return nil, err
	}

	contentByID := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		contentByID[candidate.MemoryID] = candidate.Content
	}
	for i := range response.Ranked {
		response.Ranked[i].Content = contentByID[response.Ranked[i].MemoryID]
	}
	return response.Ranked, nil
}
