package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestApplyDecayToHitsMakesL3DecayMuchSlowerThanL1(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	hits := []model.MemoryHit{
		{
			MemoryID:      "l1-old",
			SemanticScore: 0.95,
			MemoryLevel:   1,
			CreatedAtMs:   now.Add(-24 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:      "l3-old",
			SemanticScore: 0.90,
			MemoryLevel:   3,
			CreatedAtMs:   now.Add(-24 * time.Hour).UnixMilli(),
		},
	}

	applyDecayToHits(hits, 0.1, now, true)

	if hits[0].MemoryID != "l3-old" {
		t.Fatalf("expected L3 memory to outrank L1 after decay, got %q first", hits[0].MemoryID)
	}
	if hits[0].SemanticScore <= hits[1].SemanticScore {
		t.Fatalf("expected slower-decaying L3 score to remain higher, got %#v", hits)
	}
}

func TestApplyDecayToHitsReordersEmotionalHitsByRecency(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	hits := []model.MemoryHit{
		{
			MemoryID:       "older-stronger",
			EmotionalScore: 0.90,
			MemoryLevel:    1,
			CreatedAtMs:    now.Add(-12 * time.Hour).UnixMilli(),
		},
		{
			MemoryID:       "recent-slightly-weaker",
			EmotionalScore: 0.82,
			MemoryLevel:    1,
			CreatedAtMs:    now.Add(-30 * time.Minute).UnixMilli(),
		},
	}

	applyDecayToHits(hits, 0.1, now, false)

	if hits[0].MemoryID != "recent-slightly-weaker" {
		t.Fatalf("expected more recent emotional memory to move ahead after decay, got %q", hits[0].MemoryID)
	}
}

func TestDecayFactorUsesLevelSpecificRates(t *testing.T) {
	l1 := decayFactor(0.1, 1, 24)
	l2 := decayFactor(0.1, 2, 24)
	l3 := decayFactor(0.1, 3, 24)

	if !(l1 < l2 && l2 < l3) {
		t.Fatalf("expected slower decay for higher levels, got l1=%f l2=%f l3=%f", l1, l2, l3)
	}
}

func TestStepRetrieveTouchesStoredMemories(t *testing.T) {
	store := vectorstore.NewMockClient()
	if err := store.UpsertMemory(context.Background(), model.StoredMemory{
		MemoryID:         "stored-1",
		AgentID:          "agent-touch",
		Content:          "deadline escalation context",
		Emotion:          model.EmotionVector{Components: []float32{-0.2, 0.8, -0.1, 0.1, 0, 0.2}},
		Intensity:        0.7,
		CognitiveScore:   0.5,
		MemoryLevel:      2,
		AccessCount:      2,
		CreatedAtMs:      time.Now().Add(-time.Hour).UnixMilli(),
		LastAccessedAtMs: time.Now().Add(-2 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	orchestrator := New(
		emotion.NewMockClient(),
		store,
		cache.NewMockClient(),
		db.NewMockClient(),
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)
	orchestrator.SetBackgroundRunner(func(fn func()) { fn() })

	_, _, _, err := orchestrator.stepRetrieve(context.Background(), Input{
		AgentID: "agent-touch",
		Text:    "deadline issue",
	}, &FSMResult{
		NewEmotion: model.EmotionVector{Components: []float32{-0.2, 0.8, -0.1, 0.1, 0, 0.2}},
	}, model.DefaultAgentConfig("agent-touch"))
	if err != nil {
		t.Fatalf("step retrieve: %v", err)
	}

	memories, err := store.GetMemoriesByLevel(context.Background(), "agent-touch", 2, 10)
	if err != nil {
		t.Fatalf("get memories by level: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one stored memory, got %d", len(memories))
	}
	if memories[0].AccessCount != 3 {
		t.Fatalf("expected access count to increment, got %d", memories[0].AccessCount)
	}
	if memories[0].LastAccessedAtMs <= memories[0].CreatedAtMs {
		t.Fatalf("expected last_accessed_at to move forward, got created=%d last=%d", memories[0].CreatedAtMs, memories[0].LastAccessedAtMs)
	}
}
