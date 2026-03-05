package llm

import (
	"context"
	"fmt"
	"strings"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Ready(context.Context) error {
	return nil
}

func (p *MockProvider) Generate(_ context.Context, prompt string, _ GenerateOpts) (string, error) {
	trimmed := strings.TrimSpace(prompt)
	if len(trimmed) > 120 {
		trimmed = trimmed[:120]
	}
	return fmt.Sprintf("Mock response based on prompt: %s", trimmed), nil
}
