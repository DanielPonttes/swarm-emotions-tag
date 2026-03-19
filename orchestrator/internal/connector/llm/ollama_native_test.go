package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaNativeProviderReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	provider, err := NewOllamaNativeProvider(OllamaNativeConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	if err := provider.Ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
}

func TestOllamaNativeProviderStripsV1AndGenerates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var payload ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "qwen3.5:27b" {
			t.Fatalf("unexpected model: %s", payload.Model)
		}
		if payload.Think {
			t.Fatalf("expected think=false")
		}
		if payload.Options.NumPredict != 64 {
			t.Fatalf("unexpected num_predict: %d", payload.Options.NumPredict)
		}
		if len(payload.Messages) != 2 {
			t.Fatalf("unexpected message count: %d", len(payload.Messages))
		}

		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Olá!"}}`))
	}))
	defer server.Close()

	provider, err := NewOllamaNativeProvider(OllamaNativeConfig{BaseURL: server.URL + "/v1"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	response, err := provider.Generate(context.Background(), "hello", GenerateOpts{
		Model:          "Qwen/Qwen3.5-27B",
		SystemPrompt:   "Reply briefly.",
		MaxTokens:      64,
		Temperature:    0.2,
		TopP:           0.8,
		TopK:           20,
		EnableThinking: false,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response != "Olá!" {
		t.Fatalf("unexpected response: %q", response)
	}
}

func TestNormalizeOllamaModelNameMapsQwenAlias(t *testing.T) {
	if got := normalizeOllamaModelName("Qwen/Qwen3.5-27B"); got != "qwen3.5:27b" {
		t.Fatalf("unexpected normalized model: %s", got)
	}
}
