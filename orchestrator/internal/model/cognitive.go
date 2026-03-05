package model

type CognitiveContext struct {
	AgentID        string   `json:"agent_id"`
	Goals          []string `json:"goals"`
	Constraints    []string `json:"constraints"`
	WorkingSummary string   `json:"working_summary"`
	UpdatedAtMs    int64    `json:"updated_at_ms"`
}
