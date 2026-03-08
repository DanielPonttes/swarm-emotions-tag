//go:build integration

package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/testutil"
)

func TestClientIntegration_StateLockAndWorkingMemory(t *testing.T) {
	redisAddr := testutil.EnvOrDefault("REDIS_ADDR", "127.0.0.1:6379")
	testutil.RequireTCP(t, redisAddr)

	client := cache.NewClient(redisAddr)
	defer client.Close()

	ctx := context.Background()
	agentID := testutil.UniqueID("it-redis")

	state := model.DefaultAgentState(agentID)
	state.CurrentFsmState.StateName = "curious"
	state.CurrentEmotion = model.EmotionVector{Components: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}}
	if err := client.SetAgentState(ctx, agentID, state); err != nil {
		t.Fatalf("set state: %v", err)
	}

	got, err := client.GetAgentState(ctx, agentID)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if got == nil {
		t.Fatalf("expected state, got nil")
	}
	if got.CurrentFsmState.StateName != "curious" {
		t.Fatalf("expected fsm state curious, got %q", got.CurrentFsmState.StateName)
	}
	if len(got.CurrentEmotion.Components) != 6 {
		t.Fatalf("expected 6 emotion components, got %d", len(got.CurrentEmotion.Components))
	}

	locked, err := client.AcquireAgentLock(ctx, agentID, time.Second)
	if err != nil {
		t.Fatalf("acquire lock #1: %v", err)
	}
	if !locked {
		t.Fatalf("expected first lock acquisition to succeed")
	}

	lockedAgain, err := client.AcquireAgentLock(ctx, agentID, time.Second)
	if err != nil {
		t.Fatalf("acquire lock #2: %v", err)
	}
	if lockedAgain {
		t.Fatalf("expected second lock acquisition to fail while held")
	}

	if err := client.ReleaseAgentLock(ctx, agentID); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	lockedAfterRelease, err := client.AcquireAgentLock(ctx, agentID, time.Second)
	if err != nil {
		t.Fatalf("acquire lock after release: %v", err)
	}
	if !lockedAfterRelease {
		t.Fatalf("expected lock acquisition after release to succeed")
	}
	if err := client.ReleaseAgentLock(ctx, agentID); err != nil {
		t.Fatalf("release lock #2: %v", err)
	}

	entryOld := model.WorkingMemoryEntry{
		MemoryID:    "mem-old",
		Content:     "older memory",
		Score:       0.3,
		CreatedAtMs: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}
	entryNew := model.WorkingMemoryEntry{
		MemoryID:    "mem-new",
		Content:     "newer memory",
		Score:       0.9,
		CreatedAtMs: time.Now().UnixMilli(),
	}

	if err := client.PushWorkingMemory(ctx, agentID, entryOld); err != nil {
		t.Fatalf("push old working memory: %v", err)
	}
	if err := client.PushWorkingMemory(ctx, agentID, entryNew); err != nil {
		t.Fatalf("push new working memory: %v", err)
	}

	entries, err := client.GetWorkingMemory(ctx, agentID)
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	if entries[0].MemoryID != "mem-new" {
		t.Fatalf("expected newest memory first, got %q", entries[0].MemoryID)
	}
}
