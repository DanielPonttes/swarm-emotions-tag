//go:build integration

package vectorstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/testutil"
)

func TestClientIntegration_QuerySemanticAndEmotional(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant")
	client, err := vectorstore.NewClient(rawQdrantAddr, collection)
	if err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	baseURL := "http://" + hostPort
	agentID := testutil.UniqueID("agent-it-qdrant")
	otherAgentID := testutil.UniqueID("agent-other")
	if err := upsertPoints(baseURL, collection, []map[string]any{
		{
			"id":     "p1",
			"vector": []float32{0.8, 0.1, 0.0, 0.1, 0.0, 0.0},
			"payload": map[string]any{
				"agent_id":           agentID,
				"memory_id":          "mem-1",
				"content":            "deadline discussion",
				"cognitive_score":    0.42,
				"memory_level":       2,
				"is_pseudopermanent": true,
			},
		},
		{
			"id":     "p2",
			"vector": []float32{0.3, 0.2, 0.1, 0.2, 0.1, 0.1},
			"payload": map[string]any{
				"agent_id":           agentID,
				"memory_id":          "mem-2",
				"content":            "team planning context",
				"cognitive_score":    0.21,
				"memory_level":       1,
				"is_pseudopermanent": false,
			},
		},
		{
			"id":     "p-other",
			"vector": []float32{0.8, 0.1, 0.0, 0.1, 0.0, 0.0},
			"payload": map[string]any{
				"agent_id":           otherAgentID,
				"memory_id":          "mem-other",
				"content":            "foreign agent memory",
				"cognitive_score":    0.9,
				"memory_level":       3,
				"is_pseudopermanent": true,
			},
		},
	}); err != nil {
		t.Fatalf("upsert points: %v", err)
	}

	ctx := context.Background()
	semanticHits, err := waitForSemanticHits(ctx, client, agentID)
	if err != nil {
		t.Fatalf("semantic query: %v", err)
	}
	if len(semanticHits) == 0 {
		t.Fatalf("expected semantic hits")
	}
	if semanticHits[0].MemoryID == "" {
		t.Fatalf("expected semantic hit memory id")
	}
	for _, hit := range semanticHits {
		if hit.MemoryID == "mem-other" {
			t.Fatalf("expected filter by agent_id, got foreign memory in semantic results")
		}
	}

	emotionalHits, err := waitForEmotionalHits(ctx, client, agentID)
	if err != nil {
		t.Fatalf("emotional query: %v", err)
	}
	if len(emotionalHits) == 0 {
		t.Fatalf("expected emotional hits")
	}
	for _, hit := range emotionalHits {
		if hit.MemoryID == "mem-other" {
			t.Fatalf("expected filter by agent_id, got foreign memory in emotional results")
		}
	}
}

func TestClientIntegration_UpsertMemoryMakesPromotedMemoryQueryable(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant-upsert")
	client, err := vectorstore.NewClient(rawQdrantAddr, collection)
	if err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	ctx := context.Background()
	agentID := testutil.UniqueID("agent-upsert")
	memory := model.StoredMemory{
		MemoryID:       testutil.UniqueID("mem"),
		AgentID:        agentID,
		Content:        "deadline escalation and mitigation steps",
		Emotion:        model.EmotionVector{Components: []float32{-0.2, 0.9, -0.1, 0.1, 0.0, 0.2}},
		Intensity:      0.95,
		CognitiveScore: 0.81,
		MemoryLevel:    2,
		CreatedAtMs:    time.Now().UnixMilli(),
	}

	if err := client.UpsertMemory(ctx, memory); err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	semanticHits, err := waitForSemanticHits(ctx, client, agentID)
	if err != nil {
		t.Fatalf("semantic query after upsert: %v", err)
	}
	semanticHit := findMemory(semanticHits, memory.MemoryID)
	if semanticHit == nil {
		t.Fatalf("expected semantic hits to include promoted memory %q, got %#v", memory.MemoryID, semanticHits)
	}
	if semanticHit.CreatedAtMs != memory.CreatedAtMs {
		t.Fatalf("expected created_at to round-trip in semantic hits, got %d want %d", semanticHit.CreatedAtMs, memory.CreatedAtMs)
	}

	emotionalHits, err := waitForEmotionalHits(ctx, client, agentID)
	if err != nil {
		t.Fatalf("emotional query after upsert: %v", err)
	}
	emotionalHit := findMemory(emotionalHits, memory.MemoryID)
	if emotionalHit == nil {
		t.Fatalf("expected emotional hits to include promoted memory %q, got %#v", memory.MemoryID, emotionalHits)
	}
	if emotionalHit.CreatedAtMs != memory.CreatedAtMs {
		t.Fatalf("expected created_at to round-trip in emotional hits, got %d want %d", emotionalHit.CreatedAtMs, memory.CreatedAtMs)
	}
}

func TestClientIntegration_GetMemoriesByLevelAndPromoteToL3(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant-level")
	client, err := vectorstore.NewClient(rawQdrantAddr, collection)
	if err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	ctx := context.Background()
	agentID := testutil.UniqueID("agent-level")
	memory := model.StoredMemory{
		MemoryID:         testutil.UniqueID("mem"),
		AgentID:          agentID,
		Content:          "recurring escalation memory",
		Emotion:          model.EmotionVector{Components: []float32{-0.7, 0.8, -0.1, 0.2, 0, 0.1}},
		Intensity:        0.97,
		CognitiveScore:   0.84,
		MemoryLevel:      2,
		ValenceMagnitude: 0.7,
		CreatedAtMs:      time.Now().UnixMilli(),
	}

	if err := client.UpsertMemory(ctx, memory); err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	l2Memories, err := client.GetMemoriesByLevel(ctx, agentID, 2, 10)
	if err != nil {
		t.Fatalf("get l2 memories: %v", err)
	}
	l2Memory := findStoredMemory(l2Memories, memory.MemoryID)
	if l2Memory == nil {
		t.Fatalf("expected L2 memories to include %q, got %#v", memory.MemoryID, l2Memories)
	}
	if l2Memory.Intensity != memory.Intensity {
		t.Fatalf("expected intensity to round-trip, got %f want %f", l2Memory.Intensity, memory.Intensity)
	}

	if err := client.UpdateMemoryLevel(ctx, memory.MemoryID, 3); err != nil {
		t.Fatalf("promote memory to L3: %v", err)
	}

	l3Memories, err := client.GetMemoriesByLevel(ctx, agentID, 3, 10)
	if err != nil {
		t.Fatalf("get l3 memories: %v", err)
	}
	l3Memory := findStoredMemory(l3Memories, memory.MemoryID)
	if l3Memory == nil {
		t.Fatalf("expected L3 memories to include %q, got %#v", memory.MemoryID, l3Memories)
	}
	if !l3Memory.IsPseudopermanent {
		t.Fatalf("expected promoted L3 memory to be pseudopermanent")
	}
}

func TestClientIntegration_TouchMemoriesUpdatesAccessMetadata(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant-touch")
	client, err := vectorstore.NewClient(rawQdrantAddr, collection)
	if err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	ctx := context.Background()
	agentID := testutil.UniqueID("agent-touch")
	memory := model.StoredMemory{
		MemoryID:         testutil.UniqueID("mem"),
		AgentID:          agentID,
		Content:          "touch metadata memory",
		Emotion:          model.EmotionVector{Components: []float32{0.2, 0.4, 0.1, 0.1, 0, 0.2}},
		Intensity:        0.5,
		CognitiveScore:   0.6,
		MemoryLevel:      2,
		AccessCount:      1,
		CreatedAtMs:      time.Now().Add(-time.Hour).UnixMilli(),
		LastAccessedAtMs: time.Now().Add(-2 * time.Hour).UnixMilli(),
	}

	if err := client.UpsertMemory(ctx, memory); err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	touchedAt := time.Now().UnixMilli()
	if err := client.TouchMemories(ctx, []model.MemoryAccessUpdate{
		{PointID: memory.MemoryID, MemoryID: memory.MemoryID, AccessCount: 2},
	}, touchedAt); err != nil {
		t.Fatalf("touch memories: %v", err)
	}

	memories, err := client.GetMemoriesByLevel(ctx, agentID, 2, 10)
	if err != nil {
		t.Fatalf("get memories by level: %v", err)
	}
	touched := findStoredMemory(memories, memory.MemoryID)
	if touched == nil {
		t.Fatalf("expected touched memory %q, got %#v", memory.MemoryID, memories)
	}
	if touched.AccessCount != 2 {
		t.Fatalf("expected access_count=2 after touch, got %d", touched.AccessCount)
	}
	if touched.LastAccessedAtMs != touchedAt {
		t.Fatalf("expected last_accessed_at=%d after touch, got %d", touchedAt, touched.LastAccessedAtMs)
	}
}

func TestClientIntegration_DeleteStaleMemoriesRemovesExpiredL2Only(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant-gc")
	client, err := vectorstore.NewClient(rawQdrantAddr, collection)
	if err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	agentA := testutil.UniqueID("agent-gc-a")
	agentB := testutil.UniqueID("agent-gc-b")
	memories := []model.StoredMemory{
		{
			MemoryID:         testutil.UniqueID("mem-expired-a"),
			AgentID:          agentA,
			Content:          "expired low-access L2 memory",
			MemoryLevel:      2,
			AccessCount:      1,
			CreatedAtMs:      now.Add(-8 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-8 * 24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:         testutil.UniqueID("mem-expired-b"),
			AgentID:          agentB,
			Content:          "another expired low-access L2 memory",
			MemoryLevel:      2,
			AccessCount:      2,
			CreatedAtMs:      now.Add(-9 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-9 * 24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:         testutil.UniqueID("mem-frequent"),
			AgentID:          agentA,
			Content:          "expired but frequently accessed L2 memory",
			MemoryLevel:      2,
			AccessCount:      3,
			CreatedAtMs:      now.Add(-8 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-2 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:          testutil.UniqueID("mem-l3"),
			AgentID:           agentA,
			Content:           "expired L3 memory",
			MemoryLevel:       3,
			IsPseudopermanent: true,
			AccessCount:       0,
			CreatedAtMs:       now.Add(-30 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs:  now.Add(-30 * 24 * time.Hour).UnixMilli(),
		},
	}
	for _, memory := range memories {
		if err := client.UpsertMemory(ctx, memory); err != nil {
			t.Fatalf("upsert memory %s: %v", memory.MemoryID, err)
		}
	}

	deleted, err := client.DeleteStaleMemories(ctx, connector.MemoryGCParams{
		Level:            2,
		CreatedBeforeMs:  now.Add(-7 * 24 * time.Hour).UnixMilli(),
		AccessCountBelow: 3,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("delete stale memories: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted memories, got %d (%#v)", len(deleted), deleted)
	}

	l2AgentA, err := client.GetMemoriesByLevel(ctx, agentA, 2, 10)
	if err != nil {
		t.Fatalf("get L2 memories for agent A: %v", err)
	}
	if findStoredMemory(l2AgentA, memories[0].MemoryID) != nil {
		t.Fatalf("expected expired low-access memory for agent A to be deleted")
	}
	if findStoredMemory(l2AgentA, memories[2].MemoryID) == nil {
		t.Fatalf("expected frequently accessed expired memory for agent A to remain")
	}

	l2AgentB, err := client.GetMemoriesByLevel(ctx, agentB, 2, 10)
	if err != nil {
		t.Fatalf("get L2 memories for agent B: %v", err)
	}
	if findStoredMemory(l2AgentB, memories[1].MemoryID) != nil {
		t.Fatalf("expected expired low-access memory for agent B to be deleted")
	}

	l3AgentA, err := client.GetMemoriesByLevel(ctx, agentA, 3, 10)
	if err != nil {
		t.Fatalf("get L3 memories for agent A: %v", err)
	}
	if findStoredMemory(l3AgentA, memories[3].MemoryID) == nil {
		t.Fatalf("expected L3 memory to remain after GC")
	}
}

func upsertPoints(baseURL, collection string, points []map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"points": points,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/collections/%s/points?wait=true", baseURL, collection), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant upsert failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func waitForSemanticHits(ctx context.Context, client *vectorstore.Client, agentID string) ([]model.MemoryHit, error) {
	var lastErr error
	for i := 0; i < 10; i++ {
		hits, err := client.QuerySemantic(ctx, connector.QuerySemanticParams{
			AgentID: agentID,
			Text:    "deadline update",
			TopK:    10,
		})
		if err == nil && len(hits) > 0 {
			return hits, nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func waitForEmotionalHits(ctx context.Context, client *vectorstore.Client, agentID string) ([]model.MemoryHit, error) {
	var lastErr error
	for i := 0; i < 10; i++ {
		hits, err := client.QueryEmotional(ctx, connector.QueryEmotionalParams{
			AgentID: agentID,
			EmotionVector: model.EmotionVector{
				Components: []float32{0.8, 0.1, 0, 0.1, 0, 0},
			},
			TopK: 10,
		})
		if err == nil && len(hits) > 0 {
			return hits, nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func containsMemory(hits []model.MemoryHit, memoryID string) bool {
	for _, hit := range hits {
		if hit.MemoryID == memoryID {
			return true
		}
	}
	return false
}

func findMemory(hits []model.MemoryHit, memoryID string) *model.MemoryHit {
	for i := range hits {
		if hits[i].MemoryID == memoryID {
			return &hits[i]
		}
	}
	return nil
}

func findStoredMemory(memories []model.StoredMemory, memoryID string) *model.StoredMemory {
	for i := range memories {
		if memories[i].MemoryID == memoryID {
			return &memories[i]
		}
	}
	return nil
}
