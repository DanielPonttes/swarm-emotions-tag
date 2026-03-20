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
	"sort"
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

type scrollRequest struct {
	Limit       int           `json:"limit"`
	WithPayload bool          `json:"with_payload"`
	Filter      *qdrantFilter `json:"filter,omitempty"`
}

type qdrantFilter struct {
	Must []qdrantCondition `json:"must,omitempty"`
}

type qdrantCondition struct {
	Key   string                `json:"key"`
	Match *qdrantMatchCondition `json:"match,omitempty"`
	Range *qdrantRangeCondition `json:"range,omitempty"`
}

type qdrantMatchCondition struct {
	Value any `json:"value"`
}

type qdrantRangeCondition struct {
	Lt  any `json:"lt,omitempty"`
	Lte any `json:"lte,omitempty"`
}

type searchResponse struct {
	Status string        `json:"status"`
	Result []searchPoint `json:"result"`
}

type scrollResponse struct {
	Status string       `json:"status"`
	Result scrollResult `json:"result"`
}

type scrollResult struct {
	Points []searchPoint `json:"points"`
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
	return c.search(ctx, vector, params.AgentID, normalizeSearchLimit(params.TopK), true)
}

func (c *Client) QueryEmotional(ctx context.Context, params connector.QueryEmotionalParams) ([]model.MemoryHit, error) {
	vector := ensureDimension(params.EmotionVector.Components, 6)
	return c.search(ctx, vector, params.AgentID, normalizeSearchLimit(params.TopK), false)
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
	if memory.LastAccessedAtMs == 0 {
		memory.LastAccessedAtMs = memory.CreatedAtMs
	}

	payload, err := json.Marshal(map[string]any{
		"points": []map[string]any{
			{
				"id":     pointIDForMemory(memory),
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
					"access_count":       memory.AccessCount,
					"valence_magnitude":  ensureValenceMagnitude(memory),
					"created_at":         memory.CreatedAtMs,
					"last_accessed_at":   memory.LastAccessedAtMs,
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

func (c *Client) TouchMemories(ctx context.Context, touches []model.MemoryAccessUpdate, accessedAtMs int64) error {
	if len(touches) == 0 {
		return nil
	}
	if accessedAtMs == 0 {
		accessedAtMs = time.Now().UnixMilli()
	}

	for _, touch := range dedupeMemoryTouches(touches) {
		if strings.TrimSpace(touch.PointID) == "" {
			continue
		}

		payload, err := json.Marshal(map[string]any{
			"payload": map[string]any{
				"access_count":     touch.AccessCount,
				"last_accessed_at": accessedAtMs,
			},
			"points": []string{touch.PointID},
		})
		if err != nil {
			return err
		}

		err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
			callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
			defer cancel()
			url := fmt.Sprintf("%s/collections/%s/points/payload?wait=true", c.baseURL, c.collection)
			req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(payload))
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
				return fmt.Errorf("qdrant touch memory failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			return nil
		})
		if err != nil {
			c.metrics.IncDependencyError("qdrant", "touch_memories")
			return err
		}
	}

	return nil
}

func (c *Client) GetMemoriesByLevel(ctx context.Context, agentID string, level uint32, limit int) ([]model.StoredMemory, error) {
	if level == 0 {
		return nil, fmt.Errorf("memory level is required")
	}

	must := []qdrantCondition{
		{
			Key:   "memory_level",
			Match: &qdrantMatchCondition{Value: level},
		},
	}
	if strings.TrimSpace(agentID) != "" {
		must = append(must, qdrantCondition{
			Key:   "agent_id",
			Match: &qdrantMatchCondition{Value: agentID},
		})
	}

	memories, err := c.scrollMemories(ctx, scrollRequest{
		Limit:       normalizeScrollLimit(limit),
		WithPayload: true,
		Filter: &qdrantFilter{
			Must: must,
		},
	}, "get_memories_by_level")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(memories, func(i, j int) bool {
		return memories[i].CreatedAtMs > memories[j].CreatedAtMs
	})
	return memories, nil
}

func (c *Client) UpdateMemoryLevel(ctx context.Context, memoryID string, level uint32) error {
	if strings.TrimSpace(memoryID) == "" {
		return fmt.Errorf("memory_id is required")
	}
	if level == 0 {
		return fmt.Errorf("memory level is required")
	}

	payload, err := json.Marshal(map[string]any{
		"payload": map[string]any{
			"memory_level":       level,
			"is_pseudopermanent": level >= 3,
		},
		"points": []string{memoryID},
	})
	if err != nil {
		return err
	}

	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()
		url := fmt.Sprintf("%s/collections/%s/points/payload?wait=true", c.baseURL, c.collection)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(payload))
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
			return fmt.Errorf("qdrant payload update failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	})
	if err != nil {
		c.metrics.IncDependencyError("qdrant", "update_memory_level")
	}
	return err
}

func (c *Client) DeleteStaleMemories(ctx context.Context, params connector.MemoryGCParams) ([]model.StoredMemory, error) {
	if params.Level == 0 {
		return nil, fmt.Errorf("memory level is required")
	}
	if params.CreatedBeforeMs == 0 {
		return nil, fmt.Errorf("created_before_ms is required")
	}

	must := []qdrantCondition{
		{
			Key:   "memory_level",
			Match: &qdrantMatchCondition{Value: params.Level},
		},
		{
			Key:   "created_at",
			Range: &qdrantRangeCondition{Lte: params.CreatedBeforeMs},
		},
	}
	if params.AccessCountBelow > 0 {
		must = append(must, qdrantCondition{
			Key:   "access_count",
			Range: &qdrantRangeCondition{Lt: params.AccessCountBelow},
		})
	}

	memories, err := c.scrollMemories(ctx, scrollRequest{
		Limit:       normalizeScrollLimit(params.Limit),
		WithPayload: true,
		Filter: &qdrantFilter{
			Must: must,
		},
	}, "delete_stale_memories")
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}

	sort.SliceStable(memories, func(i, j int) bool {
		return memories[i].CreatedAtMs < memories[j].CreatedAtMs
	})

	pointIDs := make([]string, 0, len(memories))
	deleted := make([]model.StoredMemory, 0, len(memories))
	for _, memory := range memories {
		pointID := pointIDForMemory(memory)
		if pointID == "" {
			continue
		}
		pointIDs = append(pointIDs, pointID)
		deleted = append(deleted, memory)
	}
	if len(pointIDs) == 0 {
		return nil, nil
	}

	if err := c.deletePoints(ctx, pointIDs); err != nil {
		c.metrics.IncDependencyError("qdrant", "delete_stale_memories")
		return nil, err
	}
	return deleted, nil
}

func (c *Client) search(ctx context.Context, vector []float32, agentID string, limit int, semantic bool) ([]model.MemoryHit, error) {
	reqBody := searchRequest{
		Vector:      vector,
		Limit:       normalizeSearchLimit(limit),
		WithPayload: true,
	}
	if agentID != "" {
		reqBody.Filter = &qdrantFilter{
			Must: []qdrantCondition{
				{
					Key: "agent_id",
					Match: &qdrantMatchCondition{
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
			stored := decodeStoredMemory(point)
			hit := model.MemoryHit{
				PointID:           stored.PointID,
				MemoryID:          stored.MemoryID,
				Content:           stored.Content,
				CognitiveScore:    stored.CognitiveScore,
				MemoryLevel:       stored.MemoryLevel,
				IsPseudopermanent: stored.IsPseudopermanent,
				AccessCount:       stored.AccessCount,
				CreatedAtMs:       stored.CreatedAtMs,
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

func decodeStoredMemory(point searchPoint) model.StoredMemory {
	memory := model.StoredMemory{
		PointID:           resolvePointID(point.ID),
		MemoryID:          resolveMemoryID(point.ID, point.Payload),
		AgentID:           readString(point.Payload["agent_id"]),
		Content:           firstNonEmptyString(point.Payload["content"], point.Payload["content_text"]),
		CognitiveScore:    readFloat32(point.Payload["cognitive_score"]),
		Intensity:         readFloat32(point.Payload["intensity"]),
		MemoryLevel:       readUint32(point.Payload["memory_level"]),
		IsPseudopermanent: readBool(point.Payload["is_pseudopermanent"]),
		AccessCount:       readUint32(point.Payload["access_count"]),
		ValenceMagnitude:  readFloat32(point.Payload["valence_magnitude"]),
		CreatedAtMs:       readInt64(point.Payload["created_at"]),
		LastAccessedAtMs:  readInt64(point.Payload["last_accessed_at"]),
	}
	if memory.MemoryLevel == 0 {
		memory.MemoryLevel = 1
	}
	return memory
}

func (c *Client) scrollMemories(ctx context.Context, reqBody scrollRequest, operation string) ([]model.StoredMemory, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	memories, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.StoredMemory, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()
		url := fmt.Sprintf("%s/collections/%s/points/scroll", c.baseURL, c.collection)
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
			return nil, fmt.Errorf("qdrant scroll failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var decoded scrollResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			return nil, err
		}

		out := make([]model.StoredMemory, 0, len(decoded.Result.Points))
		for _, point := range decoded.Result.Points {
			memory := decodeStoredMemory(point)
			if memory.MemoryID == "" {
				continue
			}
			out = append(out, memory)
		}
		return out, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("qdrant", operation)
		return nil, err
	}
	return memories, nil
}

func (c *Client) deletePoints(ctx context.Context, pointIDs []string) error {
	payload, err := json.Marshal(map[string]any{
		"points": pointIDs,
	})
	if err != nil {
		return err
	}

	return resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 350*time.Millisecond)
		defer cancel()
		url := fmt.Sprintf("%s/collections/%s/points/delete?wait=true", c.baseURL, c.collection)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(payload))
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
			return fmt.Errorf("qdrant delete points failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	})
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

func normalizeSearchLimit(limit int) int {
	return clampLimit(limit, 10, 100)
}

func normalizeScrollLimit(limit int) int {
	return clampLimit(limit, 100, 1000)
}

func clampLimit(limit, fallback, max int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
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
	if pointID := resolvePointID(id); pointID != "" {
		return pointID
	}
	return ""
}

func resolvePointID(id any) string {
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

func ensureValenceMagnitude(memory model.StoredMemory) float32 {
	if memory.ValenceMagnitude > 0 {
		return memory.ValenceMagnitude
	}
	if len(memory.Emotion.Components) == 0 {
		return 0
	}
	return float32(math.Abs(float64(memory.Emotion.Components[0])))
}

func pointIDForMemory(memory model.StoredMemory) string {
	if pointID := strings.TrimSpace(memory.PointID); pointID != "" {
		return pointID
	}
	return memory.MemoryID
}

func dedupeMemoryTouches(touches []model.MemoryAccessUpdate) []model.MemoryAccessUpdate {
	if len(touches) == 0 {
		return nil
	}

	latest := make(map[string]model.MemoryAccessUpdate, len(touches))
	for _, touch := range touches {
		if strings.TrimSpace(touch.PointID) == "" {
			continue
		}
		latest[touch.PointID] = touch
	}

	out := make([]model.MemoryAccessUpdate, 0, len(latest))
	for _, touch := range latest {
		out = append(out, touch)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].PointID < out[j].PointID
	})
	return out
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

func readInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
