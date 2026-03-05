package classifier

import (
	"context"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (c *MockClient) Ready(context.Context) error {
	return nil
}

func (c *MockClient) ClassifyEmotion(_ context.Context, text string) (*connector.EmotionClassification, error) {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "urgent") || strings.Contains(lowered, "asap"):
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.2, 0.8, -0.2, 0.1, 0.0, 0.2}},
			Label:  "anxious", Confidence: 0.9, Stimulus: "urgency",
		}, nil
	case strings.Contains(lowered, "thanks") || strings.Contains(lowered, "great"):
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.7, 0.5, 0.4, 0.5, 0.3, 0.1}},
			Label:  "joyful", Confidence: 0.92, Stimulus: "praise",
		}, nil
	case strings.Contains(lowered, "problem") || strings.Contains(lowered, "wrong"):
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.5, 0.6, -0.2, -0.1, 0.2, 0.1}},
			Label:  "worried", Confidence: 0.88, Stimulus: "failure",
		}, nil
	default:
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.1, 0.2, 0.1, 0.3, 0.1, 0.5}},
			Label:  "curious", Confidence: 0.75, Stimulus: "novelty",
		}, nil
	}
}
