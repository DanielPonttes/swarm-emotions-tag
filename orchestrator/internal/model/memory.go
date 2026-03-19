package model

type MemoryHit struct {
	MemoryID          string  `json:"memory_id"`
	Content           string  `json:"content"`
	SemanticScore     float32 `json:"semantic_score"`
	EmotionalScore    float32 `json:"emotional_score"`
	CognitiveScore    float32 `json:"cognitive_score"`
	MemoryLevel       uint32  `json:"memory_level"`
	IsPseudopermanent bool    `json:"is_pseudopermanent"`
}

type ScoreCandidate struct {
	MemoryID          string  `json:"memory_id"`
	SemanticScore     float32 `json:"semantic_score"`
	EmotionalScore    float32 `json:"emotional_score"`
	CognitiveScore    float32 `json:"cognitive_score"`
	MemoryLevel       uint32  `json:"memory_level"`
	IsPseudopermanent bool    `json:"is_pseudopermanent"`
	Content           string  `json:"content,omitempty"`
}

type RankedMemory struct {
	MemoryID              string  `json:"memory_id"`
	FinalScore            float32 `json:"final_score"`
	SemanticContribution  float32 `json:"semantic_contribution"`
	EmotionalContribution float32 `json:"emotional_contribution"`
	CognitiveContribution float32 `json:"cognitive_contribution"`
	Content               string  `json:"content,omitempty"`
}

type WorkingMemoryEntry struct {
	MemoryID    string  `json:"memory_id"`
	Role        string  `json:"role,omitempty"`
	Content     string  `json:"content"`
	Score       float32 `json:"score"`
	CreatedAtMs int64   `json:"created_at_ms"`
}

type PromotionCandidate struct {
	MemoryID         string  `json:"memory_id"`
	Intensity        float32 `json:"intensity"`
	CurrentLevel     uint32  `json:"current_level"`
	AccessFrequency  uint32  `json:"access_frequency"`
	ValenceMagnitude float32 `json:"valence_magnitude"`
}

type PromotionDecision struct {
	MemoryID      string `json:"memory_id"`
	ShouldPromote bool   `json:"should_promote"`
	TargetLevel   uint32 `json:"target_level"`
	Reason        string `json:"reason"`
}
