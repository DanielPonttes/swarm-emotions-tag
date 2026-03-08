package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

type failingExecutor struct {
	err error
}

func (f failingExecutor) Execute(context.Context, pipeline.Input) (*pipeline.Output, error) {
	return nil, f.err
}

func TestInteractReturnsServiceUnavailableOnDependencyError(t *testing.T) {
	handlers := NewHandlers(
		failingExecutor{
			err: &connector.DependencyUnavailableError{
				Dependency: "emotion_engine",
			},
		},
		db.NewMockClient(),
		cache.NewMockClient(),
	)
	router := NewRouter(handlers)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/interact", bytes.NewBufferString(`{"agent_id":"agent-1","text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
