package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestPruneWorkingMemoryEntriesKeepsNewestWindow(t *testing.T) {
	entries := make([]model.WorkingMemoryEntry, 0, defaultWorkingMemoryWindow+3)
	for i := 0; i < defaultWorkingMemoryWindow+3; i++ {
		entries = append(entries, model.WorkingMemoryEntry{
			MemoryID:    fmt.Sprintf("mem-%02d", i),
			Content:     fmt.Sprintf("content-%02d", i),
			CreatedAtMs: int64(i + 1),
		})
	}

	pruned := pruneWorkingMemoryEntries(entries, defaultWorkingMemoryWindow)
	if len(pruned) != defaultWorkingMemoryWindow {
		t.Fatalf("expected %d entries, got %d", defaultWorkingMemoryWindow, len(pruned))
	}
	if pruned[0].MemoryID != fmt.Sprintf("mem-%02d", defaultWorkingMemoryWindow+2) {
		t.Fatalf("expected newest entry first, got %q", pruned[0].MemoryID)
	}
	if pruned[len(pruned)-1].MemoryID != "mem-03" {
		t.Fatalf("expected oldest retained entry to be mem-03, got %q", pruned[len(pruned)-1].MemoryID)
	}
}

func TestMockClientPushWorkingMemoryPrunesOlderEntries(t *testing.T) {
	client := NewMockClient()
	agentID := "agent-prune"

	for i := 0; i < defaultWorkingMemoryWindow+5; i++ {
		err := client.PushWorkingMemory(context.Background(), agentID, model.WorkingMemoryEntry{
			MemoryID:    fmt.Sprintf("mem-%02d", i),
			Content:     fmt.Sprintf("content-%02d", i),
			CreatedAtMs: int64(i + 1),
		})
		if err != nil {
			t.Fatalf("push working memory %d: %v", i, err)
		}
	}

	entries, err := client.GetWorkingMemory(context.Background(), agentID)
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if len(entries) != defaultWorkingMemoryWindow {
		t.Fatalf("expected %d entries after pruning, got %d", defaultWorkingMemoryWindow, len(entries))
	}
	if entries[0].MemoryID != "mem-16" {
		t.Fatalf("expected latest entry first, got %q", entries[0].MemoryID)
	}
	if entries[len(entries)-1].MemoryID != "mem-05" {
		t.Fatalf("expected oldest retained entry to be mem-05, got %q", entries[len(entries)-1].MemoryID)
	}
}

func TestMockClientWorkingMemoryExpiresAfterTTL(t *testing.T) {
	client := NewMockClientWithConfig(ClientConfig{
		WorkingMemoryTTL: 40 * time.Millisecond,
	})
	agentID := "agent-expire"

	if err := client.PushWorkingMemory(context.Background(), agentID, model.WorkingMemoryEntry{
		MemoryID: "mem-01",
		Content:  "ephemeral",
	}); err != nil {
		t.Fatalf("push working memory: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	entries, err := client.GetWorkingMemory(context.Background(), agentID)
	if err != nil {
		t.Fatalf("get working memory after ttl: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected working memory to expire, got %d entries", len(entries))
	}
}
