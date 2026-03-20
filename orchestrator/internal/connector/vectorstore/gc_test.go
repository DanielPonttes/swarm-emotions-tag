package vectorstore

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestRunMemoryGCOnceRemovesExpiredL2OnlyAndLogs(t *testing.T) {
	store := NewMockClient()
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	agentID := "agent-gc"

	seed := []model.StoredMemory{
		{
			MemoryID:         "expired-low-access",
			AgentID:          agentID,
			Content:          "delete me",
			MemoryLevel:      2,
			AccessCount:      1,
			PointID:          "expired-low-access",
			CreatedAtMs:      now.Add(-8 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-8 * 24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:         "expired-high-access",
			AgentID:          agentID,
			Content:          "keep by frequency",
			MemoryLevel:      2,
			AccessCount:      3,
			PointID:          "expired-high-access",
			CreatedAtMs:      now.Add(-8 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-8 * 24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:         "fresh-low-access",
			AgentID:          agentID,
			Content:          "keep by age",
			MemoryLevel:      2,
			AccessCount:      1,
			PointID:          "fresh-low-access",
			CreatedAtMs:      now.Add(-24 * time.Hour).UnixMilli(),
			LastAccessedAtMs: now.Add(-24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:          "expired-l3",
			AgentID:           agentID,
			Content:           "keep by level",
			MemoryLevel:       3,
			IsPseudopermanent: true,
			AccessCount:       0,
			PointID:           "expired-l3",
			CreatedAtMs:       now.Add(-30 * 24 * time.Hour).UnixMilli(),
			LastAccessedAtMs:  now.Add(-30 * 24 * time.Hour).UnixMilli(),
		},
	}
	for _, memory := range seed {
		if err := store.UpsertMemory(context.Background(), memory); err != nil {
			t.Fatalf("seed memory %s: %v", memory.MemoryID, err)
		}
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	runMemoryGCOnce(context.Background(), store, GCConfig{
		Interval:               time.Hour,
		L2MaxAge:               7 * 24 * time.Hour,
		L2AccessCountThreshold: 3,
		BatchSize:              10,
		Logger:                 logger,
		Now:                    func() time.Time { return now },
	})

	l2Memories, err := store.GetMemoriesByLevel(context.Background(), agentID, 2, 10)
	if err != nil {
		t.Fatalf("get l2 memories: %v", err)
	}
	if findStoredMemoryByID(l2Memories, "expired-low-access") != nil {
		t.Fatalf("expected expired low-access L2 memory to be removed")
	}
	if findStoredMemoryByID(l2Memories, "expired-high-access") == nil {
		t.Fatalf("expected expired high-access L2 memory to remain")
	}
	if findStoredMemoryByID(l2Memories, "fresh-low-access") == nil {
		t.Fatalf("expected fresh L2 memory to remain")
	}

	l3Memories, err := store.GetMemoriesByLevel(context.Background(), agentID, 3, 10)
	if err != nil {
		t.Fatalf("get l3 memories: %v", err)
	}
	if findStoredMemoryByID(l3Memories, "expired-l3") == nil {
		t.Fatalf("expected L3 memory to remain untouched by GC")
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "removed expired L2 memory") {
		t.Fatalf("expected GC removal log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "expired-low-access") {
		t.Fatalf("expected GC log to include removed memory id, got %q", logOutput)
	}
}

func findStoredMemoryByID(memories []model.StoredMemory, memoryID string) *model.StoredMemory {
	for i := range memories {
		if memories[i].MemoryID == memoryID {
			return &memories[i]
		}
	}
	return nil
}
