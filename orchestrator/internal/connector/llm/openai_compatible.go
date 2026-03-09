package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type OpenAICompatibleProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type chatCompletionRequest struct {
	Model           string             `json:"model"`
	Messages        []chatMessage      `json:"messages"`
	MaxTokens       int                `json:"max_tokens,omitempty"`
	Temperature     float32            `json:"temperature"`
	TopP            float32            `json:"top_p,omitempty"`
	TopK            int                `json:"top_k,omitempty"`
	PresencePenalty float32            `json:"presence_penalty,omitempty"`
	Stream          bool               `json:"stream"`
	ChatTemplate    chatTemplateKwargs `json:"chat_template_kwargs,omitempty"`
}

type chatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          json.RawMessage `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func NewOpenAICompatibleProvider(cfg OpenAICompatibleConfig) (*OpenAICompatibleProvider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("llm base url is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &OpenAICompatibleProvider{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		client:  client,
	}, nil
}

func (p *OpenAICompatibleProvider) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	p.applyHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("llm provider not ready: %s", p.readErrorBody(resp.Body))
	}
	return nil
}

func (p *OpenAICompatibleProvider) Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error) {
	model := strings.TrimSpace(opts.Model)
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

	payload := chatCompletionRequest{
		Model:           model,
		Messages:        messages,
		MaxTokens:       opts.MaxTokens,
		Temperature:     opts.Temperature,
		TopP:            opts.TopP,
		TopK:            opts.TopK,
		PresencePenalty: opts.PresencePenalty,
		Stream:          false,
		ChatTemplate: chatTemplateKwargs{
			EnableThinking: opts.EnableThinking,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	p.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("llm generate failed: %s", p.readErrorBody(resp.Body))
	}

	var decoded chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode llm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("llm response had no choices")
	}

	content := strings.TrimSpace(extractMessageContent(decoded.Choices[0].Message.Content))
	if content != "" {
		return content, nil
	}

	reasoning := strings.TrimSpace(decoded.Choices[0].Message.ReasoningContent)
	if reasoning != "" {
		return reasoning, nil
	}

	return "", fmt.Errorf("llm response content is empty")
}

func (p *OpenAICompatibleProvider) applyHeaders(req *http.Request) {
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}

func (p *OpenAICompatibleProvider) readErrorBody(body io.Reader) string {
	payload, _ := io.ReadAll(io.LimitReader(body, 2048))
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return "empty error body"
	}

	var decoded errorResponse
	if err := json.Unmarshal(payload, &decoded); err == nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return decoded.Error.Message
	}

	return string(payload)
}

func extractMessageContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteString(strings.TrimSpace(part.Text))
		}
		return builder.String()
	}

	return ""
}
