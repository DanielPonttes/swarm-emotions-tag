package vectorstore

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/resilience"
)

const defaultCollection = "memories"

type Client struct {
	baseURL    string
	collection string
	http       *http.Client
	metrics    observability.Reporter
	retry      resilience.RetryPolicy
}

type searchRequest struct {
	Vector      []float32     `json:"vector"`
	Limit       int           `json:"limit"`
	WithPayload bool          `json:"with_payload"`
	Filter      *qdrantFilter `json:"filter,omitempty"`
}

type qdrantFilter struct {
	Must []qdrantMatch `json:"must,omitempty"`
}

type qdrantMatch struct {
	Key   string               `json:"key"`
	Match qdrantMatchCondition `json:"match"`
}

type qdrantMatchCondition struct {
	Value string `json:"value"`
}

type searchResponse struct {
	Status string        `json:"status"`
	Result []searchPoint `json:"result"`
}

type searchPoint struct {
	ID      any            `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func NewClient(addr, collection string) (*Client, error) {
	baseURL, err := normalizeBaseURL(addr)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(collection) == "" {
		collection = defaultCollection
	}

	client := &Client{
		baseURL:    baseURL,
		collection: collection,
		http: &http.Client{
			Timeout: 600 * time.Millisecond,
		},
		metrics: observability.NewNoopReporter(),
		retry: resilience.RetryPolicy{
			Attempts:  3,
			BaseDelay: 15 * time.Millisecond,
			MaxDelay:  150 * time.Millisecond,
		},
	}

	setupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.ensureCollection(setupCtx); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Client) Ready(ctx context.Context) error {
	err := resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, c.baseURL+"/collections/"+c.collection, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("qdrant readiness check failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	})
	if err != nil {
		c.metrics.IncDependencyError("qdrant", "ready")
		return err
	}
	return nil
}

func (c *Client) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *Client) QuerySemantic(ctx context.Context, params connector.QuerySemanticParams) ([]model.MemoryHit, error) {
	vector := textEmbedding(params.Text, 6)
	return c.search(ctx, vector, params.AgentID, normalizeLimit(params.TopK), true)
}

func (c *Client) QueryEmotional(ctx context.Context, params connector.QueryEmotionalParams) ([]model.MemoryHit, error) {
	vector := ensureDimension(params.EmotionVector.Components, 6)
	return c.search(ctx, vector, params.AgentID, normalizeLimit(params.TopK), false)
}

func (c *Client) UpsertMemory(ctx context.Context, memory model.StoredMemory) error {
	if strings.TrimSpace(memory.MemoryID) == "" {
		return fmt.Errorf("memory_id is required")
	}
	if strings.TrimSpace(memory.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(memory.Content) == "" {
		return fmt.Errorf("content is required")
	}
	if memory.MemoryLevel == 0 {
		memory.MemoryLevel = 1
	}
	if memory.CreatedAtMs == 0 {
		memory.CreatedAtMs = time.Now().UnixMilli()
	}

	payload, err := json.Marshal(map[string]any{
		"points": []map[string]any{
			{
				"id":     memory.MemoryID,
				"vector": blendVectors(textEmbedding(memory.Content, 6), ensureDimension(memory.Emotion.Components, 6)),
				"payload": map[string]any{
					"agent_id":           memory.AgentID,
					"memory_id":          memory.MemoryID,
					"content":            memory.Content,
					"content_text":       memory.Content,
					"content_hash":       contentHash(memory.Content),
					"cognitive_score":    memory.CognitiveScore,
					"intensity":          memory.Intensity,
					"memory_level":       memory.MemoryLevel,
					"is_pseudopermanent": memory.IsPseudopermanent,
					"access_count":       0,
					"created_at":         memory.CreatedAtMs,
					"last_accessed_at":   memory.CreatedAtMs,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()

		url := fmt.Sprintf("%s/collections/%s/points?wait=true", c.baseURL, c.collection)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPut, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return fmt.Errorf("qdrant upsert failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	})
	if err != nil {
		c.metrics.IncDependencyError("qdrant", "upsert_memory")
	}
	return err
}

func (c *Client) search(ctx context.Context, vector []float32, agentID string, limit int, semantic bool) ([]model.MemoryHit, error) {
	reqBody := searchRequest{
		Vector:      vector,
		Limit:       limit,
		WithPayload: true,
	}
	if agentID != "" {
		reqBody.Filter = &qdrantFilter{
			Must: []qdrantMatch{
				{
					Key: "agent_id",
					Match: qdrantMatchCondition{
						Value: agentID,
					},
				},
			},
		}
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	hits, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.MemoryHit, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()
		url := fmt.Sprintf("%s/collections/%s/points/search", c.baseURL, c.collection)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return nil, fmt.Errorf("qdrant search failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var decoded searchResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			return nil, err
		}

		result := make([]model.MemoryHit, 0, len(decoded.Result))
		for _, point := range decoded.Result {
			hit := model.MemoryHit{
				MemoryID:          resolveMemoryID(point.ID, point.Payload),
				Content:           firstNonEmptyString(point.Payload["content"], point.Payload["content_text"]),
				CognitiveScore:    readFloat32(point.Payload["cognitive_score"]),
				MemoryLevel:       readUint32(point.Payload["memory_level"]),
				IsPseudopermanent: readBool(point.Payload["is_pseudopermanent"]),
			}
			if hit.MemoryID == "" {
				continue
			}
			if hit.MemoryLevel == 0 {
				hit.MemoryLevel = 1
			}
			if semantic {
				hit.SemanticScore = float32(point.Score)
			} else {
				hit.EmotionalScore = float32(point.Score)
			}
			result = append(result, hit)
		}
		return result, nil
	})
	if err != nil {
		op := "query_emotional"
		if semantic {
			op = "query_semantic"
		}
		c.metrics.IncDependencyError("qdrant", op)
		return nil, err
	}
	return hits, nil
}

func (c *Client) ensureCollection(ctx context.Context) error {
	payload := map[string]any{
		"vectors": map[string]any{
			"size":     6,
			"distance": "Cosine",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(
			callCtx,
			http.MethodPut,
			fmt.Sprintf("%s/collections/%s", c.baseURL, c.collection),
			bytes.NewReader(body),
		)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			return fmt.Errorf("ensure qdrant collection failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
		}
		return nil
	})
}

func normalizeBaseURL(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("qdrant address is required")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}

	parsed, err := url.Parse(addr)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("invalid qdrant address: %q", addr)
	}
	port := parsed.Port()
	switch port {
	case "":
		port = "6333"
	case "6334":
		// In docker-compose the configured env currently points to gRPC.
		// HTTP API runs on the sibling port.
		port = "6333"
	}

	parsed.Scheme = "http"
	parsed.Host = net.JoinHostPort(host, port)
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func ensureDimension(components []float32, size int) []float32 {
	if len(components) == size {
		return append([]float32(nil), components...)
	}
	out := make([]float32, size)
	copy(out, components)
	return out
}

func textEmbedding(text string, dimension int) []float32 {
	embedding := make([]float32, dimension)
	for i, r := range strings.ToLower(text) {
		embedding[i%dimension] += float32((int(r)%31)+1) / 31
	}
	var norm float64
	for _, value := range embedding {
		norm += float64(value * value)
	}
	if norm == 0 {
		return embedding
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range embedding {
		embedding[i] *= scale
	}
	return embedding
}

func blendVectors(vectors ...[]float32) []float32 {
	size := 0
	for _, vector := range vectors {
		if len(vector) > size {
			size = len(vector)
		}
	}
	if size == 0 {
		return nil
	}

	out := make([]float32, size)
	for _, vector := range vectors {
		for i, value := range vector {
			out[i] += value
		}
	}
	var norm float64
	for _, value := range out {
		norm += float64(value * value)
	}
	if norm == 0 {
		return out
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range out {
		out[i] *= scale
	}
	return out
}

func resolveMemoryID(id any, payload map[string]any) string {
	if payloadID := readString(payload["memory_id"]); payloadID != "" {
		return payloadID
	}
	switch value := id.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	default:
		return ""
	}
}

func contentHash(content string) string {
	sum := sha1.Sum([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if text := readString(value); text != "" {
			return text
		}
	}
	return ""
}

func readString(value any) string {
	text, _ := value.(string)
	return text
}

func readFloat32(value any) float32 {
	switch typed := value.(type) {
	case float64:
		return float32(typed)
	case float32:
		return typed
	case int:
		return float32(typed)
	default:
		return 0
	}
}

func readBool(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func readUint32(value any) uint32 {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return uint32(typed)
	case int:
		if typed < 0 {
			return 0
		}
		return uint32(typed)
	case int64:
		if typed < 0 {
			return 0
		}
		return uint32(typed)
	default:
		return 0
	}
}
