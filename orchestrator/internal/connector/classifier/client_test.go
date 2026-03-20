package classifier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

func TestClientClassifyEmotion(t *testing.T) {
	var healthTraceID string
	var classifyTraceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			healthTraceID = r.Header.Get(traceIDHeader)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","model_loaded":true,"classifier_mode":"heuristic","model_name":"test-model"}`))
		case "/classify-emotion":
			classifyTraceID = r.Header.Get(traceIDHeader)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"emotion_vector":[0.1,0.2,0.3,0.4,0.5,0.6],"label":"gratitude","confidence":0.95}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := tracectx.WithTraceID(context.Background(), "req-123")
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}

	result, err := client.ClassifyEmotion(ctx, "thanks for the help")
	if err != nil {
		t.Fatalf("classify emotion: %v", err)
	}
	if result.Label != "gratitude" {
		t.Fatalf("expected gratitude, got %s", result.Label)
	}
	if result.Stimulus != "praise" {
		t.Fatalf("expected stimulus praise, got %s", result.Stimulus)
	}
	if healthTraceID != "req-123" {
		t.Fatalf("expected ready request to propagate trace header, got %q", healthTraceID)
	}
	if classifyTraceID != "req-123" {
		t.Fatalf("expected classify request to propagate trace header, got %q", classifyTraceID)
	}
}

func TestClientReadyFailsWhenModelIsNotLoaded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"degraded","model_loaded":false,"classifier_mode":"transformers","model_name":"broken-model","load_error":"weights missing"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if err := client.Ready(context.Background()); err == nil || err.Error() != "classifier model not loaded: weights missing" {
		t.Fatalf("unexpected ready error: %v", err)
	}
}
