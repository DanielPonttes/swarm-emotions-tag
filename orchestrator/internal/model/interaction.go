package model

type InteractionRequest struct {
	AgentID  string         `json:"agent_id"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type InteractionResponse struct {
	Response     string        `json:"response"`
	EmotionState EmotionVector `json:"emotion_state"`
	FsmState     string        `json:"fsm_state"`
	Intensity    float32       `json:"intensity"`
	LatencyMs    int64         `json:"latency_ms"`
	TraceID      string        `json:"trace_id,omitempty"`
}

type InteractionLog struct {
	AgentID      string        `json:"agent_id"`
	UserText     string        `json:"user_text"`
	ResponseText string        `json:"response_text"`
	Stimulus     string        `json:"stimulus"`
	FsmState     FsmState      `json:"fsm_state"`
	Emotion      EmotionVector `json:"emotion"`
	Intensity    float32       `json:"intensity"`
	CreatedAtMs  int64         `json:"created_at_ms"`
}

type EmotionHistoryEntry struct {
	AgentID     string        `json:"agent_id"`
	FsmState    FsmState      `json:"fsm_state"`
	Emotion     EmotionVector `json:"emotion"`
	Intensity   float32       `json:"intensity"`
	CreatedAtMs int64         `json:"created_at_ms"`
}
