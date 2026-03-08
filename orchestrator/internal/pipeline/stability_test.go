//go:build stability

package pipeline

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
)

func TestStability_NoGoroutineLeakUnderLoad(t *testing.T) {
	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	orchestrator := New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cacheClient,
		dbClient,
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)
	// Executa post-process inline para reduzir ruído do teste de leak.
	orchestrator.SetBackgroundRunner(func(fn func()) { fn() })

	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	workers := 20
	iterationsPerWorker := 100

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterationsPerWorker)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterationsPerWorker; i++ {
				_, err := orchestrator.Execute(ctx, Input{
					AgentID: "agent-stability",
					Text:    "stability test load",
				})
				if err != nil {
					errCh <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("execute under load: %v", err)
		}
	}

	runtime.GC()
	time.Sleep(300 * time.Millisecond)
	after := runtime.NumGoroutine()

	delta := after - baseline
	if delta > 8 {
		t.Fatalf("possible goroutine leak: baseline=%d after=%d delta=%d", baseline, after, delta)
	}
}
