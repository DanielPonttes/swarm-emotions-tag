package emotion

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (c *MockClient) TransitionState(_ context.Context, req *connector.TransitionRequest) (*connector.TransitionResponse, error) {
	newState := req.CurrentState
	newState.EnteredAt = time.Now().UnixMilli()
	newState.StateName, newState.MacroState = transitionForStimulus(req.CurrentState.StateName, req.Stimulus)
	return &connector.TransitionResponse{
		NewState:           newState,
		TransitionOccurred: newState.StateName != req.CurrentState.StateName,
	}, nil
}

func (c *MockClient) ComputeEmotionVector(_ context.Context, req *connector.ComputeRequest) (*connector.ComputeResponse, error) {
	components := make([]float32, len(req.CurrentEmotion.Components))
	copy(components, req.CurrentEmotion.Components)
	for i := range components {
		trigger := float32(0)
		if i < len(req.Trigger.Components) {
			trigger = req.Trigger.Components[i]
		}
		weight := float32(0.1)
		index := i*req.WDimension + i
		if req.WDimension > 0 && index < len(req.WMatrix) {
			weight = req.WMatrix[index]
		}
		components[i] = clamp(req.Baseline.Components[i] + (components[i]-req.Baseline.Components[i])*(1-req.DecayLambda) + weight*trigger)
	}
	result := model.EmotionVector{Components: components}
	return &connector.ComputeResponse{
		NewEmotion: result,
		Intensity:  result.Intensity(),
	}, nil
}

func (c *MockClient) FuseScores(_ context.Context, req *connector.FuseRequest) (*connector.FuseResponse, error) {
	ranked := make([]model.RankedMemory, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		semantic := req.Weights.Alpha * candidate.SemanticScore
		emotional := req.Weights.Beta * candidate.EmotionalScore
		cognitive := req.Weights.Gamma * candidate.CognitiveScore
		final := semantic + emotional + cognitive
		if candidate.IsPseudopermanent {
			final *= 1 + req.Weights.PseudopermBoost
		}
		ranked = append(ranked, model.RankedMemory{
			MemoryID:              candidate.MemoryID,
			FinalScore:            final,
			SemanticContribution:  semantic,
			EmotionalContribution: emotional,
			CognitiveContribution: cognitive,
			Content:               candidate.Content,
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].FinalScore > ranked[j].FinalScore
	})
	return &connector.FuseResponse{Ranked: ranked}, nil
}

func (c *MockClient) EvaluatePromotion(_ context.Context, req *connector.PromotionRequest) (*connector.PromotionResponse, error) {
	decisions := make([]model.PromotionDecision, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		shouldPromote := candidate.Intensity > req.IntensityThreshold ||
			(candidate.AccessFrequency > req.FrequencyThreshold && candidate.ValenceMagnitude > req.ValenceThreshold)
		target := candidate.CurrentLevel
		reason := "Below thresholds"
		if shouldPromote {
			target++
			if target > 3 {
				target = 3
			}
			reason = "Promotion thresholds reached"
		}
		decisions = append(decisions, model.PromotionDecision{
			MemoryID: candidate.MemoryID, ShouldPromote: shouldPromote, TargetLevel: target, Reason: reason,
		})
	}
	return &connector.PromotionResponse{Decisions: decisions}, nil
}

func (c *MockClient) ProcessInteraction(ctx context.Context, req *connector.ProcessRequest) (*connector.ProcessResponse, error) {
	transition, err := c.TransitionState(ctx, &connector.TransitionRequest{
		AgentID: req.AgentID, CurrentState: req.CurrentState, Stimulus: req.Stimulus, StimulusVector: req.StimulusVector,
	})
	if err != nil {
		return nil, err
	}
	compute, err := c.ComputeEmotionVector(ctx, &connector.ComputeRequest{
		CurrentEmotion: req.CurrentEmotion,
		Trigger:        req.StimulusVector,
		Baseline:       req.Config.Baseline,
		WMatrix:        req.Config.WMatrix,
		WDimension:     req.Config.WDimension,
		DecayLambda:    req.Config.DecayLambda,
		DeltaTime:      1.0,
		EnableNoise:    req.Config.NoiseEnabled,
		NoiseSigma:     req.Config.NoiseSigma,
	})
	if err != nil {
		return nil, err
	}
	fused, err := c.FuseScores(ctx, &connector.FuseRequest{
		Candidates: req.ScoreCandidates, Weights: req.Config.Weights, CurrentEmotion: compute.NewEmotion,
	})
	if err != nil {
		return nil, err
	}
	promotions, err := c.EvaluatePromotion(ctx, &connector.PromotionRequest{
		Candidates:         req.PromotionCandidates,
		IntensityThreshold: 0.9,
		FrequencyThreshold: 10,
		ValenceThreshold:   0.8,
	})
	if err != nil {
		return nil, err
	}
	return &connector.ProcessResponse{
		NewState:           transition.NewState,
		TransitionOccurred: transition.TransitionOccurred,
		NewEmotion:         compute.NewEmotion,
		NewIntensity:       compute.Intensity,
		Ranked:             fused.Ranked,
		Promotions:         promotions.Decisions,
	}, nil
}

func transitionForStimulus(current, stimulus string) (stateName, macro string) {
	switch strings.ToLower(stimulus) {
	case "praise":
		return "joyful", "positive"
	case "urgency":
		return "anxious", "negative"
	case "failure", "severe_criticism":
		return "worried", "negative"
	case "resolution":
		return "calm", "neutral"
	case "user_frustration":
		return "empathetic", "positive"
	default:
		if current == "neutral" {
			return "curious", "positive"
		}
		if current == "" {
			return "neutral", "neutral"
		}
		switch current {
		case "joyful", "curious", "empathetic":
			return current, "positive"
		case "worried", "frustrated", "anxious":
			return current, "negative"
		default:
			return current, "neutral"
		}
	}
}

func clamp(value float32) float32 {
	switch {
	case value < -1:
		return -1
	case value > 1:
		return 1
	default:
		return value
	}
}
