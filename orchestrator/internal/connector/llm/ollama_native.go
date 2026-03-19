package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
)

type OllamaNativeConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

type OllamaNativeProvider struct {
	baseURL string
	client  *http.Client
}

type ollamaChatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Think    bool           `json:"think"`
	Options  ollamaChatOpts `json:"options,omitempty"`
}

type ollamaChatOpts struct {
	Temperature     float32 `json:"temperature,omitempty"`
	TopP            float32 `json:"top_p,omitempty"`
	TopK            int     `json:"top_k,omitempty"`
	PresencePenalty float32 `json:"presence_penalty,omitempty"`
	NumPredict      int     `json:"num_predict,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error"`
}

func NewOllamaNativeProvider(cfg OllamaNativeConfig) (*OllamaNativeProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("llm base url is required")
	}
	baseURL = strings.TrimSuffix(baseURL, "/v1")

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &OllamaNativeProvider{
		baseURL: baseURL,
		client:  client,
	}, nil
}

func (p *OllamaNativeProvider) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ollama provider not ready: %s", readBody(resp.Body))
	}
	return nil
}

func (p *OllamaNativeProvider) Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error) {
	model := normalizeOllamaModelName(opts.Model)
	if model == "" {
		return "", fmt.Errorf("llm model is required")
	}

	messages := make([]chatMessage, 0, 2)
	if systemPrompt := strings.TrimSpace(opts.SystemPrompt); systemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	messages = append(messages, chatMessage{
		Role:    "user",
		Content: prompt,
	})

	payload := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Think:    opts.EnableThinking,
		Options: ollamaChatOpts{
			Temperature:     opts.Temperature,
			TopP:            opts.TopP,
			TopK:            opts.TopK,
			PresencePenalty: opts.PresencePenalty,
			NumPredict:      opts.MaxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama generate failed: %s", readBody(resp.Body))
	}

	var decoded ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}

	content := strings.TrimSpace(decoded.Message.Content)
	if content != "" {
		return content, nil
	}

	content = strings.TrimSpace(decoded.Response)
	if content != "" {
		return content, nil
	}

	return "", fmt.Errorf("ollama response content is empty")
}

func (p *OllamaNativeProvider) GenerateStream(ctx context.Context, prompt string, opts GenerateOpts) (<-chan connector.StreamChunk, error) {
	model := normalizeOllamaModelName(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("llm model is required")
	}

	messages := make([]chatMessage, 0, 2)
	if systemPrompt := strings.TrimSpace(opts.SystemPrompt); systemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	messages = append(messages, chatMessage{
		Role:    "user",
		Content: prompt,
	})

	payload := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Think:    opts.EnableThinking,
		Options: ollamaChatOpts{
			Temperature:     opts.Temperature,
			TopP:            opts.TopP,
			TopK:            opts.TopK,
			PresencePenalty: opts.PresencePenalty,
			NumPredict:      opts.MaxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("ollama generate failed: %s", readBody(resp.Body))
	}

	ch := make(chan connector.StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var decoded ollamaChatResponse
			if err := json.Unmarshal([]byte(line), &decoded); err != nil {
				ch <- connector.StreamChunk{Error: fmt.Errorf("decode ollama stream chunk: %w", err)}
				return
			}
			if decoded.Error != "" {
				ch <- connector.StreamChunk{Error: fmt.Errorf("ollama stream failed: %s", decoded.Error)}
				return
			}

			text := strings.TrimSpace(decoded.Message.Content)
			if text == "" {
				text = strings.TrimSpace(decoded.Response)
			}
			if text != "" {
				ch <- connector.StreamChunk{Text: text}
			}
			if decoded.Done {
				ch <- connector.StreamChunk{Done: true}
				return
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			ch <- connector.StreamChunk{Error: fmt.Errorf("read ollama stream: %w", err)}
			return
		}
		ch <- connector.StreamChunk{Done: true}
	}()

	return ch, nil
}

func readBody(body io.Reader) string {
	payload, _ := io.ReadAll(io.LimitReader(body, 2048))
	return strings.TrimSpace(string(payload))
}

func normalizeOllamaModelName(model string) string {
	trimmed := strings.TrimSpace(model)
	lower := strings.ToLower(trimmed)

	switch lower {
	case "qwen/qwen3.5-27b", "qwen3.5-27b", "qwen 3.5 27b", "qwen3.5 27b":
		return "qwen3.5:27b"
	}

	if strings.HasPrefix(lower, "qwen/") && strings.Contains(lower, "3.5") && strings.Contains(lower, "27b") {
		return "qwen3.5:27b"
	}
	return trimmed
}
