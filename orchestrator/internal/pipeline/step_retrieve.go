package pipeline

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"golang.org/x/sync/errgroup"
)

func (o *Orchestrator) stepRetrieve(
	ctx context.Context,
	input Input,
	fsmResult *FSMResult,
	agentConfig *model.AgentConfig,
) ([]model.ScoreCandidate, *model.CognitiveContext, []model.WorkingMemoryEntry, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.25, 400000000)
	defer cancel()

	group, groupCtx := errgroup.WithContext(stepCtx)

	var semanticResults []model.MemoryHit
	var emotionalResults []model.MemoryHit
	var cognitiveContext *model.CognitiveContext
	var workingMemory []model.WorkingMemoryEntry

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

	group.Go(func() error {
		var err error
		workingMemory, err = o.cache.GetWorkingMemory(groupCtx, input.AgentID)
		return err
	})

	if err := group.Wait(); err != nil {
		return nil, nil, nil, err
	}

	decayLambda := float32(0)
	if agentConfig != nil {
		decayLambda = agentConfig.DecayLambda
	}
	now := time.Now()
	applyDecayToHits(semanticResults, decayLambda, now, true)
	applyDecayToHits(emotionalResults, decayLambda, now, false)

	cognitiveContext = prepareCognitiveContext(cognitiveContext, input.Text, fsmResult.NewEmotion)
	candidates := mergeResults(semanticResults, emotionalResults)
	applyCognitiveReranking(candidates, cognitiveContext, input.Text)

	return candidates, cognitiveContext, workingMemory, nil
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

func applyDecayToHits(hits []model.MemoryHit, decayLambda float32, now time.Time, semantic bool) {
	if len(hits) == 0 || decayLambda <= 0 {
		return
	}

	for i := range hits {
		if hits[i].CreatedAtMs <= 0 {
			continue
		}

		createdAt := time.UnixMilli(hits[i].CreatedAtMs)
		if createdAt.After(now) {
			continue
		}

		ageHours := float32(now.Sub(createdAt).Hours())
		factor := decayFactor(decayLambda, hits[i].MemoryLevel, ageHours)
		if semantic {
			hits[i].SemanticScore *= factor
		} else {
			hits[i].EmotionalScore *= factor
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if semantic {
			return hits[i].SemanticScore > hits[j].SemanticScore
		}
		return hits[i].EmotionalScore > hits[j].EmotionalScore
	})
}

func decayFactor(decayLambda float32, memoryLevel uint32, ageHours float32) float32 {
	if decayLambda <= 0 || ageHours <= 0 {
		return 1
	}
	return float32(math.Exp(float64(-adjustedDecayLambda(decayLambda, memoryLevel) * ageHours)))
}

func adjustedDecayLambda(decayLambda float32, memoryLevel uint32) float32 {
	switch memoryLevel {
	case 3:
		return decayLambda * 0.01
	case 2:
		return decayLambda * 0.1
	default:
		return decayLambda
	}
}
