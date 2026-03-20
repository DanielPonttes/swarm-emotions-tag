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

func TestInferStimulusExpandedCoverage(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		label    string
		expected string
	}{
		{name: "urgency from text", text: "This is urgent, please handle asap", label: "neutral", expected: "urgency"},
		{name: "resolution from text", text: "The issue is fixed now and resolved", label: "neutral", expected: "resolution"},
		{name: "success from text", text: "It worked, success confirmed", label: "neutral", expected: "success"},
		{name: "empathy from text", text: "I understand this is hard, sorry this happened", label: "neutral", expected: "empathy"},
		{name: "user frustration from text", text: "I'm frustrated and stuck again", label: "neutral", expected: "user_frustration"},
		{name: "boredom from text", text: "This is getting boring and repetitive", label: "neutral", expected: "boredom"},
		{name: "severe criticism from text", text: "This rollout is unacceptable and terrible", label: "neutral", expected: "severe_criticism"},
		{name: "mild criticism from label", text: "I am disappointed with this answer", label: "disappointment", expected: "mild_criticism"},
		{name: "empathy from label", text: "neutral text", label: "caring", expected: "empathy"},
		{name: "resolution from label", text: "neutral text", label: "relief", expected: "resolution"},
		{name: "novelty from curiosity label", text: "neutral text", label: "curiosity", expected: "novelty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferStimulus(tc.text, tc.label); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
