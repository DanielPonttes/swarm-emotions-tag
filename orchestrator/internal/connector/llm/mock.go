package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/connector"
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

func (p *MockProvider) GenerateStream(ctx context.Context, prompt string, opts GenerateOpts) (<-chan connector.StreamChunk, error) {
	full, err := p.Generate(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}

	ch := make(chan connector.StreamChunk, 8)
	go func() {
		defer close(ch)
		for _, part := range splitStreamText(full, 32) {
			select {
			case <-ctx.Done():
				ch <- connector.StreamChunk{Error: ctx.Err()}
				return
			case ch <- connector.StreamChunk{Text: part}:
			}
		}
		ch <- connector.StreamChunk{Done: true}
	}()
	return ch, nil
}
