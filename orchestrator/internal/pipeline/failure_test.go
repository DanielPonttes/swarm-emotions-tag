package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
)

func TestExecuteFailsWithUnavailableCacheDependency(t *testing.T) {
	cacheClient := cache.NewClient("127.0.0.1:6399")
	defer cacheClient.Close()

	orchestrator := New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cacheClient,
		db.NewMockClient(),
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := orchestrator.Execute(ctx, Input{
		AgentID: "agent-cache-down",
		Text:    "hello",
	})
	if err == nil {
		t.Fatalf("expected error when cache is unavailable")
	}
	if !strings.Contains(err.Error(), "acquire agent lock") {
		t.Fatalf("expected acquire agent lock failure, got: %v", err)
	}
}

func TestExecuteReturnsDependencyUnavailableWhenEmotionFails(t *testing.T) {
	errDependency := &connector.DependencyUnavailableError{
		Dependency: "emotion_engine",
		Cause:      errors.New("forced downtime"),
	}
	orchestrator := New(
		failingEmotionClient{err: errDependency},
		vectorstore.NewMockClient(),
		cache.NewMockClient(),
		db.NewMockClient(),
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)

	_, err := orchestrator.Execute(context.Background(), Input{
		AgentID: "agent-emotion-down",
		Text:    "hello",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !connector.IsDependencyUnavailable(err) {
		t.Fatalf("expected dependency unavailable error, got: %v", err)
	}
}

type failingEmotionClient struct {
	err error
}

func (f failingEmotionClient) TransitionState(context.Context, *connector.TransitionRequest) (*connector.TransitionResponse, error) {
	return nil, f.err
}

func (f failingEmotionClient) ComputeEmotionVector(context.Context, *connector.ComputeRequest) (*connector.ComputeResponse, error) {
	return nil, f.err
}

func (f failingEmotionClient) FuseScores(context.Context, *connector.FuseRequest) (*connector.FuseResponse, error) {
	return nil, f.err
}

func (f failingEmotionClient) EvaluatePromotion(context.Context, *connector.PromotionRequest) (*connector.PromotionResponse, error) {
	return nil, f.err
}

func (f failingEmotionClient) ProcessInteraction(context.Context, *connector.ProcessRequest) (*connector.ProcessResponse, error) {
	return nil, f.err
}
