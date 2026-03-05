package vectorstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (c *MockClient) QuerySemantic(_ context.Context, params connector.QuerySemanticParams) ([]model.MemoryHit, error) {
	base := float32(0.55)
	if strings.Contains(strings.ToLower(params.Text), "deadline") {
		base = 0.75
	}
	return []model.MemoryHit{
		{
			MemoryID:       fmt.Sprintf("%s-semantic-1", params.AgentID),
			Content:        "Previous relevant task discussion",
			SemanticScore:  base,
			CognitiveScore: 0.25,
			MemoryLevel:    2,
		},
		{
			MemoryID:       fmt.Sprintf("%s-semantic-2", params.AgentID),
			Content:        "User preference about concise answers",
			SemanticScore:  base - 0.1,
			CognitiveScore: 0.20,
			MemoryLevel:    1,
		},
	}, nil
}

func (c *MockClient) QueryEmotional(_ context.Context, params connector.QueryEmotionalParams) ([]model.MemoryHit, error) {
	intensity := params.EmotionVector.Intensity()
	return []model.MemoryHit{
		{
			MemoryID:          fmt.Sprintf("%s-emotional-1", params.AgentID),
			Content:           "Memory aligned with current emotional tone",
			EmotionalScore:    minFloat32(0.9, 0.4+intensity*0.2),
			IsPseudopermanent: true,
			MemoryLevel:       2,
		},
		{
			MemoryID:       fmt.Sprintf("%s-emotional-2", params.AgentID),
			Content:        "Secondary emotionally similar memory",
			EmotionalScore: minFloat32(0.8, 0.3+intensity*0.15),
			MemoryLevel:    1,
		},
	}, nil
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
