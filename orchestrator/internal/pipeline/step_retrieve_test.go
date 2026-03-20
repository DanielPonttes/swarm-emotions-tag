package pipeline

import (
	"testing"
	"time"

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
