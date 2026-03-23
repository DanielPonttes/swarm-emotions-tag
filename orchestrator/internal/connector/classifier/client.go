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
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

const traceIDHeader = "X-Trace-Id"

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

type healthResponse struct {
	Status                         string            `json:"status"`
	ModelLoaded                    bool              `json:"model_loaded"`
	ClassifierMode                 string            `json:"classifier_mode"`
	ModelName                      string            `json:"model_name"`
	ClassifierDevice               string            `json:"classifier_device"`
	ClassifierBatchSize            int               `json:"classifier_batch_size"`
	ClassifierOllamaMaxConcurrency int               `json:"classifier_ollama_max_concurrency"`
	LoadError                      string            `json:"load_error"`
	Runtime                        healthRuntimeInfo `json:"runtime"`
}

type healthRuntimeInfo struct {
	TorchVersion    string   `json:"torch_version"`
	CUDAAvailable   bool     `json:"cuda_available"`
	CUDADeviceCount int      `json:"cuda_device_count"`
	CUDADevices     []string `json:"cuda_devices"`
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
	applyTraceHeaders(req, ctx)
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
	var decoded healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		c.metrics.IncDependencyError("classifier", "ready")
		return fmt.Errorf("decode classifier health: %w", err)
	}
	if !decoded.ModelLoaded {
		c.metrics.IncDependencyError("classifier", "ready")
		if strings.TrimSpace(decoded.LoadError) != "" {
			return fmt.Errorf("classifier model not loaded: %s (%s)", decoded.LoadError, decoded.summary())
		}
		return fmt.Errorf("classifier model not loaded (%s)", decoded.summary())
	}
	return nil
}

func (h healthResponse) summary() string {
	parts := make([]string, 0, 8)
	if value := strings.TrimSpace(h.Status); value != "" {
		parts = append(parts, "status="+value)
	}
	if value := strings.TrimSpace(h.ClassifierMode); value != "" {
		parts = append(parts, "mode="+value)
	}
	if value := strings.TrimSpace(h.ModelName); value != "" {
		parts = append(parts, "model="+value)
	}
	if value := strings.TrimSpace(h.ClassifierDevice); value != "" {
		parts = append(parts, "device="+value)
	}
	if h.ClassifierBatchSize > 0 {
		parts = append(parts, fmt.Sprintf("batch_size=%d", h.ClassifierBatchSize))
	}
	if h.ClassifierOllamaMaxConcurrency > 0 {
		parts = append(parts, fmt.Sprintf("ollama_concurrency=%d", h.ClassifierOllamaMaxConcurrency))
	}
	if h.Runtime.CUDAAvailable {
		parts = append(parts, fmt.Sprintf("cuda_devices=%d", h.Runtime.CUDADeviceCount))
	}
	if value := strings.TrimSpace(h.Runtime.TorchVersion); value != "" {
		parts = append(parts, "torch="+value)
	}
	return strings.Join(parts, ", ")
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
	applyTraceHeaders(req, ctx)

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
	normalizedLabel := strings.ToLower(strings.TrimSpace(label))
	switch {
	case containsAny(lowered, "urgent", "asap", "immediately", "right now", "deadline", "blocker", "sev1", "p1"):
		return "urgency"
	case containsAny(lowered, "resolved", "fixed", "solved", "working now", "works now", "all good now", "issue is gone"):
		return "resolution"
	case containsAny(lowered, "success", "worked", "it works", "passou", "passed", "completed successfully", "done successfully"):
		return "success"
	case containsAny(lowered, "i understand", "understand this is hard", "that sounds hard", "sorry you're dealing with this", "sorry this happened", "take your time"):
		return "empathy"
	case containsAny(lowered, "i'm frustrated", "im frustrated", "frustrated again", "this is frustrating", "i'm annoyed", "im annoyed", "stuck again"):
		return "user_frustration"
	case containsAny(lowered, "boring", "repetitive", "same thing again", "nothing new", "stale"):
		return "boredom"
	case containsAny(lowered, "unacceptable", "terrible", "awful", "horrible", "disaster", "catastrophic"):
		return "severe_criticism"
	case containsAny(lowered, "thanks", "thank you", "great", "appreciate", "grateful", "nice work"):
		return "praise"
	case containsAny(lowered, "problem", "wrong", "failed", "mistake", "broken", "error"):
		return "failure"
	case normalizedLabel == "gratitude" || normalizedLabel == "admiration" || normalizedLabel == "approval" || normalizedLabel == "joy" || normalizedLabel == "optimism" || normalizedLabel == "excitement":
		return "praise"
	case normalizedLabel == "caring":
		return "empathy"
	case normalizedLabel == "relief" || normalizedLabel == "realization":
		return "resolution"
	case normalizedLabel == "surprise" || normalizedLabel == "curiosity":
		return "novelty"
	case normalizedLabel == "fear" || normalizedLabel == "nervousness" || normalizedLabel == "confusion":
		return "urgency"
	case normalizedLabel == "anger" || normalizedLabel == "annoyance" || normalizedLabel == "disappointment" || normalizedLabel == "disapproval" || normalizedLabel == "sadness" || normalizedLabel == "grief":
		return "mild_criticism"
	default:
		return "novelty"
	}
}

func containsAny(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func applyTraceHeaders(req *http.Request, ctx context.Context) {
	if req == nil {
		return
	}
	if traceID := tracectx.TraceID(ctx); traceID != "" {
		req.Header.Set(traceIDHeader, traceID)
	}
}
