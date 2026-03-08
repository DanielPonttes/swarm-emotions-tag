package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
)

type Client struct {
	baseURL string
	http    *http.Client
	metrics observability.Reporter
}

type classifyRequest struct {
	Text string `json:"text"`
}

type classifyResponse struct {
	EmotionVector []float32 `json:"emotion_vector"`
	Label         string    `json:"label"`
	Confidence    float32   `json:"confidence"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 2 * time.Second,
		},
		metrics: observability.NewNoopReporter(),
	}
}

func (c *Client) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.metrics.IncDependencyError("classifier", "ready")
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.metrics.IncDependencyError("classifier", "ready")
		return fmt.Errorf("classifier health returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ClassifyEmotion(ctx context.Context, text string) (*connector.EmotionClassification, error) {
	payload, err := json.Marshal(classifyRequest{Text: text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/classify-emotion", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.metrics.IncDependencyError("classifier", "classify_emotion")
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.metrics.IncDependencyError("classifier", "classify_emotion")
		return nil, fmt.Errorf("classifier returned %d", resp.StatusCode)
	}

	var decoded classifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		c.metrics.IncDependencyError("classifier", "classify_emotion")
		return nil, err
	}
	return &connector.EmotionClassification{
		Vector:     model.EmotionVector{Components: decoded.EmotionVector},
		Label:      decoded.Label,
		Confidence: decoded.Confidence,
		Stimulus:   inferStimulus(text, decoded.Label),
	}, nil
}

func inferStimulus(text, label string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "urgent") || strings.Contains(lowered, "asap"):
		return "urgency"
	case strings.Contains(lowered, "thanks") || strings.Contains(lowered, "great"):
		return "praise"
	case strings.Contains(lowered, "problem") || strings.Contains(lowered, "wrong"):
		return "failure"
	case label == "joyful":
		return "praise"
	case label == "worried" || label == "frustrated":
		return "mild_criticism"
	default:
		return "novelty"
	}
}
