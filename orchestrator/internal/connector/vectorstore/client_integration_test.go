//go:build integration

package vectorstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/testutil"
)

func TestClientIntegration_EnsureCollectionCreatesNamedVectorsAndPayloadIndexes(t *testing.T) {
	rawQdrantAddr := testutil.EnvOrDefault("QDRANT_ADDR", "127.0.0.1:6333")
	hostPort, err := testutil.ExtractHostPort(rawQdrantAddr, "6333")
	if err != nil {
		t.Fatalf("extract qdrant host: %v", err)
	}
	if strings.HasSuffix(hostPort, ":6334") {
		hostPort = strings.TrimSuffix(hostPort, ":6334") + ":6333"
	}
	testutil.RequireTCP(t, hostPort)

	collection := testutil.UniqueID("it-qdrant-schema")
	if _, err := vectorstore.NewClient(rawQdrantAddr, collection); err != nil {
		t.Fatalf("new vectorstore client: %v", err)
	}

	info, err := getCollectionInfo("http://"+hostPort, collection)
	if err != nil {
		t.Fatalf("get collection info: %v", err)
	}

	vectors, ok := nestedMap(info, "result", "config", "params", "vectors")
	if !ok {
		t.Fatalf("expected named vectors in collection config, got %#v", info)
	}
	semantic, ok := nestedMap(vectors, "semantic")
	if !ok {
		t.Fatalf("expected semantic named vector, got %#v", vectors)
	}
	if got := int(readFloat64(semantic["size"])); got != 768 {
		t.Fatalf("expected semantic vector size 768, got %d", got)
	}
	emotional, ok := nestedMap(vectors, "emotional")
	if !ok {
		t.Fatalf("expected emotional named vector, got %#v", vectors)
	}
	if got := int(readFloat64(emotional["size"])); got != 6 {
		t.Fatalf("expected emotional vector size 6, got %d", got)
	}

	payloadSchema, ok := nestedMap(info, "result", "payload_schema")
	if !ok {
		t.Fatalf("expected payload schema to be present, got %#v", info)
	}
	for _, field := range []string{"agent_id", "memory_level", "intensity", "created_at"} {
		if _, ok := payloadSchema[field]; !ok {
			t.Fatalf("expected payload index for %q, got %#v", field, payloadSchema)
		}
	}
}

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

	agentID := testutil.UniqueID("agent-it-qdrant")
	otherAgentID := testutil.UniqueID("agent-other")
	for _, memory := range []model.StoredMemory{
		{
			MemoryID:          "mem-1",
			AgentID:           agentID,
			Content:           "deadline discussion",
			CognitiveScore:    0.42,
			MemoryLevel:       2,
			IsPseudopermanent: true,
			Emotion:           model.EmotionVector{Components: []float32{0.1, 0.3, 0.2, 0, 0, 0}},
			CreatedAtMs:       time.Now().Add(-2 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:       "mem-2",
			AgentID:        agentID,
			Content:        "team planning context",
			CognitiveScore: 0.21,
			MemoryLevel:    1,
			Emotion:        model.EmotionVector{Components: []float32{0.2, 0.1, 0.1, 0.1, 0, 0.2}},
			CreatedAtMs:    time.Now().Add(-time.Hour).UnixMilli(),
		},
		{
			MemoryID:          "mem-other",
			AgentID:           otherAgentID,
			Content:           "foreign agent memory",
			CognitiveScore:    0.9,
			MemoryLevel:       3,
			IsPseudopermanent: true,
			Emotion:           model.EmotionVector{Components: []float32{0.7, 0.1, 0, 0, 0, 0}},
			CreatedAtMs:       time.Now().UnixMilli(),
		},
	} {
		if err := client.UpsertMemory(context.Background(), memory); err != nil {
			t.Fatalf("upsert memory %s: %v", memory.MemoryID, err)
		}
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

	memories, err := client.GetMemoriesByLevel(ctx, agentID, 2, 10)
	if err != nil {
		t.Fatalf("get memories by level before touch: %v", err)
	}
	stored := findStoredMemory(memories, memory.MemoryID)
	if stored == nil {
		t.Fatalf("expected stored memory %q before touch, got %#v", memory.MemoryID, memories)
	}

	touchedAt := time.Now().UnixMilli()
	if err := client.TouchMemories(ctx, []model.MemoryAccessUpdate{
		{PointID: stored.PointID, MemoryID: memory.MemoryID, AccessCount: 2},
	}, touchedAt); err != nil {
		t.Fatalf("touch memories: %v", err)
	}

	memories, err = client.GetMemoriesByLevel(ctx, agentID, 2, 10)
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
	deletedIDs := []string{deleted[0].MemoryID, deleted[1].MemoryID}
	slices.Sort(deletedIDs)
	expectedDeletedIDs := []string{memories[0].MemoryID, memories[1].MemoryID}
	slices.Sort(expectedDeletedIDs)
	if !slices.Equal(deletedIDs, expectedDeletedIDs) {
		t.Fatalf("expected deleted ids %v, got %v", expectedDeletedIDs, deletedIDs)
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

	semanticHits, err := waitForSemanticHits(ctx, client, agentA)
	if err != nil {
		t.Fatalf("semantic query after GC: %v", err)
	}
	if containsMemory(semanticHits, memories[0].MemoryID) {
		t.Fatalf("expected deleted memory %q to disappear from semantic results", memories[0].MemoryID)
	}

	emotionalHits, err := waitForEmotionalHits(ctx, client, agentA)
	if err != nil {
		t.Fatalf("emotional query after GC: %v", err)
	}
	if containsMemory(emotionalHits, memories[0].MemoryID) {
		t.Fatalf("expected deleted memory %q to disappear from emotional results", memories[0].MemoryID)
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

func getCollectionInfo(baseURL, collection string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/collections/%s", baseURL, collection), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("qdrant collection info failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
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

func nestedMap(root map[string]any, path ...string) (map[string]any, bool) {
	current := root
	for idx, key := range path {
		value, ok := current[key]
		if !ok {
			return nil, false
		}
		if idx == len(path)-1 {
			next, ok := value.(map[string]any)
			return next, ok
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

func readFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	default:
		return 0
	}
}
