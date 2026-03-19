package model

import "fmt"

type CognitiveGoal struct {
	ID          string  `json:"id,omitempty"`
	Description string  `json:"description"`
	Priority    float32 `json:"priority"`
}

type CognitiveBeliefs struct {
	UserExpertise         string    `json:"user_expertise,omitempty"`
	TaskComplexity        string    `json:"task_complexity,omitempty"`
	TimePressure          bool      `json:"time_pressure,omitempty"`
	UserEmotionalEstimate []float32 `json:"user_emotional_estimate,omitempty"`
	WorkingSummary        string    `json:"working_summary,omitempty"`
}

type CognitiveNorms struct {
	Constraints             []string `json:"constraints,omitempty"`
	FormalityLevel          string   `json:"formality_level,omitempty"`
	HonestyCommitment       string   `json:"honesty_commitment,omitempty"`
	EmotionalExpressiveness string   `json:"emotional_expressiveness,omitempty"`
}

type CognitiveContext struct {
	AgentID           string            `json:"agent_id"`
	Goals             []string          `json:"goals,omitempty"`
	ActiveGoals       []CognitiveGoal   `json:"active_goals,omitempty"`
	Constraints       []string          `json:"constraints,omitempty"`
	Norms             CognitiveNorms    `json:"norms,omitempty"`
	Beliefs           CognitiveBeliefs  `json:"beliefs,omitempty"`
	ConversationPhase string            `json:"conversation_phase,omitempty"`
	WorkingSummary    string            `json:"working_summary,omitempty"`
	UpdatedAtMs       int64             `json:"updated_at_ms"`
}

func DefaultCognitiveContext(agentID string) *CognitiveContext {
	ctx := &CognitiveContext{
		AgentID: agentID,
		Goals: []string{
			"Resolve the user's request",
			"Preserve emotional continuity",
		},
		Constraints: []string{
			"Stay concise",
			"Do not invent facts",
		},
		ActiveGoals: []CognitiveGoal{
			{ID: "resolve_request", Description: "Resolve the user's request", Priority: 1.0},
			{ID: "emotional_continuity", Description: "Preserve emotional continuity", Priority: 0.8},
		},
		Norms: CognitiveNorms{
			Constraints:             []string{"Stay concise", "Do not invent facts"},
			FormalityLevel:          "balanced",
			HonestyCommitment:       "strict",
			EmotionalExpressiveness: "calibrated",
		},
		Beliefs: CognitiveBeliefs{
			UserExpertise:  "unknown",
			TaskComplexity: "medium",
		},
		ConversationPhase: "idle",
	}
	ctx.Normalize()
	return ctx
}

func (c *CognitiveContext) Normalize() {
	if c == nil {
		return
	}
	if c.ConversationPhase == "" {
		c.ConversationPhase = "idle"
	}
	if c.Norms.FormalityLevel == "" {
		c.Norms.FormalityLevel = "balanced"
	}
	if c.Norms.HonestyCommitment == "" {
		c.Norms.HonestyCommitment = "strict"
	}
	if c.Norms.EmotionalExpressiveness == "" {
		c.Norms.EmotionalExpressiveness = "calibrated"
	}
	if c.Beliefs.UserExpertise == "" {
		c.Beliefs.UserExpertise = "unknown"
	}
	if c.Beliefs.TaskComplexity == "" {
		c.Beliefs.TaskComplexity = "medium"
	}

	if len(c.ActiveGoals) == 0 && len(c.Goals) > 0 {
		c.ActiveGoals = make([]CognitiveGoal, 0, len(c.Goals))
		for i, goal := range c.Goals {
			priority := 1.0 - float32(i)*0.15
			if priority < 0.35 {
				priority = 0.35
			}
			c.ActiveGoals = append(c.ActiveGoals, CognitiveGoal{
				ID:          fmt.Sprintf("goal_%d", i+1),
				Description: goal,
				Priority:    priority,
			})
		}
	}
	if len(c.Goals) == 0 && len(c.ActiveGoals) > 0 {
		c.Goals = make([]string, 0, len(c.ActiveGoals))
		for _, goal := range c.ActiveGoals {
			if goal.Description != "" {
				c.Goals = append(c.Goals, goal.Description)
			}
		}
	}

	if len(c.Norms.Constraints) == 0 && len(c.Constraints) > 0 {
		c.Norms.Constraints = append([]string(nil), c.Constraints...)
	}
	if len(c.Constraints) == 0 && len(c.Norms.Constraints) > 0 {
		c.Constraints = append([]string(nil), c.Norms.Constraints...)
	}

	if c.WorkingSummary == "" {
		c.WorkingSummary = c.Beliefs.WorkingSummary
	}
	if c.Beliefs.WorkingSummary == "" {
		c.Beliefs.WorkingSummary = c.WorkingSummary
	}
}

func (c *CognitiveContext) Clone() *CognitiveContext {
	if c == nil {
		return nil
	}
	cloned := *c
	cloned.Goals = append([]string(nil), c.Goals...)
	cloned.Constraints = append([]string(nil), c.Constraints...)
	cloned.ActiveGoals = append([]CognitiveGoal(nil), c.ActiveGoals...)
	cloned.Beliefs.UserEmotionalEstimate = append([]float32(nil), c.Beliefs.UserEmotionalEstimate...)
	cloned.Norms.Constraints = append([]string(nil), c.Norms.Constraints...)
	cloned.Normalize()
	return &cloned
}
