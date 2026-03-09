package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleProviderReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3.5-27B"}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	if err := provider.Ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
}

func TestOpenAICompatibleProviderGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("expected bearer auth, got %q", got)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req["model"] != "Qwen/Qwen3.5-27B" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}
		if req["max_tokens"] != float64(192) {
			t.Fatalf("unexpected max_tokens: %#v", req["max_tokens"])
		}
		if req["temperature"] != float64(0.2) {
			t.Fatalf("unexpected temperature: %#v", req["temperature"])
		}
		if req["top_p"] != float64(0.8) {
			t.Fatalf("unexpected top_p: %#v", req["top_p"])
		}
		if req["top_k"] != float64(20) {
			t.Fatalf("unexpected top_k: %#v", req["top_k"])
		}

		chatTemplate, ok := req["chat_template_kwargs"].(map[string]any)
		if !ok {
			t.Fatalf("expected chat_template_kwargs object, got %#v", req["chat_template_kwargs"])
		}
		if chatTemplate["enable_thinking"] != false {
			t.Fatalf("expected enable_thinking false, got %#v", chatTemplate["enable_thinking"])
		}

		messages, ok := req["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %#v", req["messages"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"qwen response"}}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	result, err := provider.Generate(context.Background(), "hello", GenerateOpts{
		Model:           "Qwen/Qwen3.5-27B",
		SystemPrompt:    "be concise",
		MaxTokens:       192,
		Temperature:     0.2,
		TopP:            0.8,
		TopK:            20,
		PresencePenalty: 0,
		EnableThinking:  false,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result != "qwen response" {
		t.Fatalf("unexpected response: %q", result)
	}
}

func TestOpenAICompatibleProviderGenerateReturnsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model unavailable"}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: server.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.Generate(context.Background(), "hello", GenerateOpts{
		Model: "Qwen/Qwen3.5-27B",
	})
	if err == nil || err.Error() != "llm generate failed: model unavailable" {
		t.Fatalf("unexpected error: %v", err)
	}
}
