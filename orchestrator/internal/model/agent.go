package model

import "time"

type ScoreWeights struct {
	Alpha           float32 `json:"alpha"`
	Beta            float32 `json:"beta"`
	Gamma           float32 `json:"gamma"`
	PseudopermBoost float32 `json:"pseudoperm_boost"`
}

type AgentConfig struct {
	AgentID      string        `json:"agent_id"`
	DisplayName  string        `json:"display_name"`
	Baseline     EmotionVector `json:"baseline"`
	WMatrix      []float32     `json:"w_matrix"`
	WDimension   int           `json:"w_dimension"`
	Weights      ScoreWeights  `json:"weights"`
	DecayLambda  float32       `json:"decay_lambda"`
	NoiseEnabled bool          `json:"noise_enabled"`
	NoiseSigma   float32       `json:"noise_sigma"`
}

type AgentState struct {
	AgentID         string        `json:"agent_id"`
	CurrentEmotion  EmotionVector `json:"current_emotion"`
	CurrentFsmState FsmState      `json:"current_fsm_state"`
	UpdatedAtMs     int64         `json:"updated_at_ms"`
}

func DefaultAgentConfig(agentID string) *AgentConfig {
	return &AgentConfig{
		AgentID:     agentID,
		DisplayName: agentID,
		Baseline: EmotionVector{
			Components: []float32{0, 0, 0, 0.5, 0, 0},
		},
		WMatrix: []float32{
			0.1, 0, 0, 0, 0, 0,
			0, 0.1, 0, 0, 0, 0,
			0, 0, 0.1, 0, 0, 0,
			0, 0, 0, 0.1, 0, 0,
			0, 0, 0, 0, 0.1, 0,
			0, 0, 0, 0, 0, 0.1,
		},
		WDimension:  6,
		Weights:     ScoreWeights{Alpha: 0.4, Beta: 0.3, Gamma: 0.3, PseudopermBoost: 0.2},
		DecayLambda: 0.1,
	}
}

func DefaultAgentState(agentID string) *AgentState {
	return &AgentState{
		AgentID: agentID,
		CurrentEmotion: EmotionVector{
			Components: []float32{0, 0, 0, 0.5, 0, 0},
		},
		CurrentFsmState: FsmState{
			StateName:  "neutral",
			MacroState: "neutral",
			EnteredAt:  time.Now().UnixMilli(),
		},
		UpdatedAtMs: time.Now().UnixMilli(),
	}
}
