package vectorstore

import (
	"context"
	"log/slog"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
)

const (
	defaultGCInterval             = time.Hour
	defaultGCL2MaxAge             = 7 * 24 * time.Hour
	defaultGCAccessCountThreshold = 3
	defaultGCBatchSize            = 100
)

type GCConfig struct {
	Interval               time.Duration
	L2MaxAge               time.Duration
	L2AccessCountThreshold uint32
	BatchSize              int
	Logger                 *slog.Logger
	Now                    func() time.Time
}

func StartMemoryGC(ctx context.Context, store connector.VectorStoreClient, cfg GCConfig) {
	if store == nil {
		return
	}

	cfg = normalizeGCConfig(cfg)
	go func() {
		runMemoryGCOnce(ctx, store, cfg)

		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runMemoryGCOnce(ctx, store, cfg)
			}
		}
	}()
}

func runMemoryGCOnce(ctx context.Context, store connector.VectorStoreClient, cfg GCConfig) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cutoff := cfg.Now().Add(-cfg.L2MaxAge).UnixMilli()
		deleted, err := store.DeleteStaleMemories(ctx, connector.MemoryGCParams{
			Level:            2,
			CreatedBeforeMs:  cutoff,
			AccessCountBelow: cfg.L2AccessCountThreshold,
			Limit:            cfg.BatchSize,
		})
		if err != nil {
			cfg.Logger.Warn("memory GC failed", "level", 2, "error", err)
			return
		}
		if len(deleted) == 0 {
			return
		}

		for _, memory := range deleted {
			cfg.Logger.Info(
				"removed expired L2 memory",
				"agent_id", memory.AgentID,
				"memory_id", memory.MemoryID,
				"point_id", pointIDForMemory(memory),
				"created_at_ms", memory.CreatedAtMs,
				"access_count", memory.AccessCount,
			)
		}
		if len(deleted) < cfg.BatchSize {
			return
		}
	}
}

func normalizeGCConfig(cfg GCConfig) GCConfig {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultGCInterval
	}
	if cfg.L2MaxAge <= 0 {
		cfg.L2MaxAge = defaultGCL2MaxAge
	}
	if cfg.L2AccessCountThreshold == 0 {
		cfg.L2AccessCountThreshold = defaultGCAccessCountThreshold
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultGCBatchSize
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}
