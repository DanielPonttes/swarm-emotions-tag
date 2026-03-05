package pipeline

import (
	"context"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"golang.org/x/sync/errgroup"
)

func (o *Orchestrator) stepRetrieve(
	ctx context.Context,
	input Input,
	fsmResult *FSMResult,
	_ *model.AgentConfig,
) ([]model.ScoreCandidate, *model.CognitiveContext, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.25, 400000000)
	defer cancel()

	group, groupCtx := errgroup.WithContext(stepCtx)

	var semanticResults []model.MemoryHit
	var emotionalResults []model.MemoryHit
	var cognitiveContext *model.CognitiveContext

	group.Go(func() error {
		var err error
		semanticResults, err = o.vectorStore.QuerySemantic(groupCtx, connector.QuerySemanticParams{
			AgentID: input.AgentID,
			Text:    input.Text,
			TopK:    10,
		})
		return err
	})

	group.Go(func() error {
		var err error
		emotionalResults, err = o.vectorStore.QueryEmotional(groupCtx, connector.QueryEmotionalParams{
			AgentID:       input.AgentID,
			EmotionVector: fsmResult.NewEmotion,
			TopK:          10,
		})
		return err
	})

	group.Go(func() error {
		var err error
		cognitiveContext, err = o.db.GetCognitiveContext(groupCtx, input.AgentID)
		return err
	})

	if err := group.Wait(); err != nil {
		return nil, nil, err
	}

	return mergeResults(semanticResults, emotionalResults), cognitiveContext, nil
}

func mergeResults(semanticResults, emotionalResults []model.MemoryHit) []model.ScoreCandidate {
	merged := make(map[string]model.ScoreCandidate)

	for _, hit := range semanticResults {
		candidate := merged[hit.MemoryID]
		candidate.MemoryID = hit.MemoryID
		candidate.Content = hit.Content
		candidate.SemanticScore = hit.SemanticScore
		candidate.CognitiveScore = hit.CognitiveScore
		candidate.MemoryLevel = hit.MemoryLevel
		candidate.IsPseudopermanent = hit.IsPseudopermanent
		merged[hit.MemoryID] = candidate
	}

	for _, hit := range emotionalResults {
		candidate := merged[hit.MemoryID]
		candidate.MemoryID = hit.MemoryID
		if candidate.Content == "" {
			candidate.Content = hit.Content
		}
		candidate.EmotionalScore = hit.EmotionalScore
		if hit.MemoryLevel > candidate.MemoryLevel {
			candidate.MemoryLevel = hit.MemoryLevel
		}
		candidate.IsPseudopermanent = candidate.IsPseudopermanent || hit.IsPseudopermanent
		merged[hit.MemoryID] = candidate
	}

	out := make([]model.ScoreCandidate, 0, len(merged))
	for _, candidate := range merged {
		out = append(out, candidate)
	}
	return out
}
