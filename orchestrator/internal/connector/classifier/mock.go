package classifier

import (
	"context"

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
	switch inferStimulus(text, "neutral") {
	case "urgency":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.2, 0.8, -0.2, 0.1, 0.0, 0.2}},
			Label:  "anxious", Confidence: 0.9, Stimulus: "urgency",
		}, nil
	case "resolution":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.4, -0.2, 0.2, 0.5, 0.2, 0.0}},
			Label:  "relief", Confidence: 0.9, Stimulus: "resolution",
		}, nil
	case "success":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.8, 0.4, 0.5, 0.6, 0.3, 0.1}},
			Label:  "joy", Confidence: 0.9, Stimulus: "success",
		}, nil
	case "empathy":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.5, 0.2, 0.1, 0.3, 0.8, 0.1}},
			Label:  "caring", Confidence: 0.88, Stimulus: "empathy",
		}, nil
	case "user_frustration":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.6, 0.6, -0.2, 0.2, -0.2, 0.0}},
			Label:  "annoyance", Confidence: 0.9, Stimulus: "user_frustration",
		}, nil
	case "boredom":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.1, -0.2, 0.0, 0.1, 0.0, -0.3}},
			Label:  "neutral", Confidence: 0.75, Stimulus: "boredom",
		}, nil
	case "severe_criticism":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.8, 0.8, 0.2, 0.4, -0.5, -0.2}},
			Label:  "anger", Confidence: 0.93, Stimulus: "severe_criticism",
		}, nil
	case "praise":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.7, 0.5, 0.4, 0.5, 0.3, 0.1}},
			Label:  "joyful", Confidence: 0.92, Stimulus: "praise",
		}, nil
	case "failure":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.5, 0.6, -0.2, -0.1, 0.2, 0.1}},
			Label:  "worried", Confidence: 0.88, Stimulus: "failure",
		}, nil
	case "mild_criticism":
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{-0.4, 0.4, -0.1, 0.3, -0.1, 0.0}},
			Label:  "disappointment", Confidence: 0.85, Stimulus: "mild_criticism",
		}, nil
	default:
		return &connector.EmotionClassification{
			Vector: model.EmotionVector{Components: []float32{0.1, 0.2, 0.1, 0.3, 0.1, 0.5}},
			Label:  "curious", Confidence: 0.75, Stimulus: "novelty",
		}, nil
	}
}
