package vectorstore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/resilience"
)

func TestNewClientCreatesNamedVectorCollectionAndPayloadIndexes(t *testing.T) {
	var (
		collectionBody map[string]any
		indexFields    []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/collections/test-collection":
			if err := json.NewDecoder(r.Body).Decode(&collectionBody); err != nil {
				t.Fatalf("decode collection body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/test-collection/index":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode index body: %v", err)
			}
			indexFields = append(indexFields, payload["field_name"].(string))
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	if _, err := NewClient(server.URL, "test-collection"); err != nil {
		t.Fatalf("new client: %v", err)
	}

	vectors, ok := collectionBody["vectors"].(map[string]any)
	if !ok {
		t.Fatalf("expected collection vectors map, got %#v", collectionBody)
	}
	semantic, ok := vectors[semanticVectorName].(map[string]any)
	if !ok {
		t.Fatalf("expected semantic vector config, got %#v", vectors)
	}
	if got := int(semantic["size"].(float64)); got != semanticVectorSize {
		t.Fatalf("expected semantic size %d, got %d", semanticVectorSize, got)
	}
	emotional, ok := vectors[emotionalVectorName].(map[string]any)
	if !ok {
		t.Fatalf("expected emotional vector config, got %#v", vectors)
	}
	if got := int(emotional["size"].(float64)); got != emotionalVectorSize {
		t.Fatalf("expected emotional size %d, got %d", emotionalVectorSize, got)
	}

	slices.Sort(indexFields)
	expected := []string{"agent_id", "created_at", "intensity", "memory_level"}
	if !slices.Equal(indexFields, expected) {
		t.Fatalf("expected payload indexes %v, got %v", expected, indexFields)
	}
}

func TestUpsertMemoryUsesNamedVectors(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/collections/test/points" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestHTTPClient(server.URL, "test", server.Client())
	err := client.UpsertMemory(context.Background(), model.StoredMemory{
		MemoryID: "memory-1",
		AgentID:  "agent-1",
		Content:  "deadline escalation",
		Emotion:  model.EmotionVector{Components: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}},
	})
	if err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	points := body["points"].([]any)
	point := points[0].(map[string]any)
	vectors := point["vector"].(map[string]any)
	semantic := vectors[semanticVectorName].([]any)
	if len(semantic) != semanticVectorSize {
		t.Fatalf("expected semantic vector size %d, got %d", semanticVectorSize, len(semantic))
	}
	emotional := vectors[emotionalVectorName].([]any)
	if len(emotional) != emotionalVectorSize {
		t.Fatalf("expected emotional vector size %d, got %d", emotionalVectorSize, len(emotional))
	}
}

func TestQueryUsesNamedVectorSelectors(t *testing.T) {
	type capturedRequest struct {
		Using  string        `json:"using"`
		Query  []float32     `json:"query"`
		Filter *qdrantFilter `json:"filter"`
	}

	var requests []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/test/points/query" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode search payload: %v", err)
		}
		requests = append(requests, payload)
		_, _ = io.WriteString(w, `{"status":"ok","result":{"points":[{"id":"p1","score":0.9,"payload":{"agent_id":"agent-1","memory_id":"memory-1","content":"deadline escalation","memory_level":2}}]}}`)
	}))
	defer server.Close()

	client := newTestHTTPClient(server.URL, "test", server.Client())
	if _, err := client.QuerySemantic(context.Background(), connector.QuerySemanticParams{
		AgentID: "agent-1",
		Text:    "deadline",
		TopK:    5,
	}); err != nil {
		t.Fatalf("query semantic: %v", err)
	}
	if _, err := client.QueryEmotional(context.Background(), connector.QueryEmotionalParams{
		AgentID:       "agent-1",
		EmotionVector: model.EmotionVector{Components: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}},
		TopK:          5,
	}); err != nil {
		t.Fatalf("query emotional: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected 2 search requests, got %d", len(requests))
	}
	if requests[0].Using != semanticVectorName {
		t.Fatalf("expected semantic vector name, got %q", requests[0].Using)
	}
	if len(requests[0].Query) != semanticVectorSize {
		t.Fatalf("expected semantic query vector size %d, got %d", semanticVectorSize, len(requests[0].Query))
	}
	if requests[1].Using != emotionalVectorName {
		t.Fatalf("expected emotional vector name, got %q", requests[1].Using)
	}
	if len(requests[1].Query) != emotionalVectorSize {
		t.Fatalf("expected emotional query vector size %d, got %d", emotionalVectorSize, len(requests[1].Query))
	}
	for _, request := range requests {
		if request.Filter == nil || len(request.Filter.Must) != 1 || request.Filter.Must[0].Key != "agent_id" {
			t.Fatalf("expected agent_id filter, got %#v", request.Filter)
		}
	}
}

func TestDeleteStaleMemoriesDeletesWholePoint(t *testing.T) {
	var deletedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/collections/test/points/scroll":
			_, _ = io.WriteString(w, `{"status":"ok","result":{"points":[{"id":"point-1","payload":{"agent_id":"agent-1","memory_id":"memory-1","content":"expired","memory_level":2,"created_at":1}}]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/collections/test/points/delete":
			if err := json.NewDecoder(r.Body).Decode(&deletedBody); err != nil {
				t.Fatalf("decode delete body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestHTTPClient(server.URL, "test", server.Client())
	deleted, err := client.DeleteStaleMemories(context.Background(), connector.MemoryGCParams{
		Level:            2,
		CreatedBeforeMs:  10,
		AccessCountBelow: 3,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("delete stale memories: %v", err)
	}
	if len(deleted) != 1 || deleted[0].MemoryID != "memory-1" {
		t.Fatalf("expected deleted memory-1, got %#v", deleted)
	}
	points := deletedBody["points"].([]any)
	if len(points) != 1 || points[0].(string) != normalizePointID("point-1") {
		t.Fatalf("expected full point deletion for point-1, got %#v", deletedBody)
	}
}

func newTestHTTPClient(baseURL, collection string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		collection: collection,
		http:       httpClient,
		metrics:    observability.NewNoopReporter(),
		retry: resilience.RetryPolicy{
			Attempts: 1,
		},
	}
}
