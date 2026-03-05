package pipeline

import (
	"context"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type FSMResult struct {
	NewEmotion         model.EmotionVector
	NewFsmState        model.FsmState
	NewIntensity       float32
	TransitionOccurred bool
	Stimulus           string
}

func (o *Orchestrator) stepFSMAndVector(
	ctx context.Context,
	agentState *model.AgentState,
	agentConfig *model.AgentConfig,
	stimulusVector []float32,
	stimulusType string,
) (*FSMResult, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.2, 300000000)
	defer cancel()

	transition, err := o.emotionClient.TransitionState(stepCtx, &connector.TransitionRequest{
		AgentID:      agentState.AgentID,
		CurrentState: agentState.CurrentFsmState,
		Stimulus:     stimulusType,
		StimulusVector: model.EmotionVector{
			Components: append([]float32(nil), stimulusVector...),
		},
	})
	if err != nil {
		return nil, err
	}

	compute, err := o.emotionClient.ComputeEmotionVector(stepCtx, &connector.ComputeRequest{
		CurrentEmotion: agentState.CurrentEmotion,
		Trigger:        model.EmotionVector{Components: append([]float32(nil), stimulusVector...)},
		Baseline:       agentConfig.Baseline,
		WMatrix:        agentConfig.WMatrix,
		WDimension:     agentConfig.WDimension,
		DecayLambda:    agentConfig.DecayLambda,
		DeltaTime:      1.0,
		EnableNoise:    agentConfig.NoiseEnabled,
		NoiseSigma:     agentConfig.NoiseSigma,
	})
	if err != nil {
		return nil, err
	}

	return &FSMResult{
		NewEmotion:         compute.NewEmotion,
		NewFsmState:        transition.NewState,
		NewIntensity:       compute.Intensity,
		TransitionOccurred: transition.TransitionOccurred,
		Stimulus:           stimulusType,
	}, nil
}
