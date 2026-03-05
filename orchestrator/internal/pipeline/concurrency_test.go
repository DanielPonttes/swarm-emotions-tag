package pipeline

import (
	"context"
	"sync"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestAgentLockPreventsLostUpdates(t *testing.T) {
	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	if err := dbClient.SaveAgentConfig(context.Background(), model.DefaultAgentConfig("agent-99")); err != nil {
		t.Fatalf("save config: %v", err)
	}

	orchestrator := New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cacheClient,
		dbClient,
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)
	orchestrator.SetBackgroundRunner(func(fn func()) { fn() })

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := orchestrator.Execute(context.Background(), Input{
				AgentID: "agent-99",
				Text:    "thanks for handling this",
			})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}
	}

	history, err := dbClient.GetEmotionHistory(context.Background(), "agent-99")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 10 {
		t.Fatalf("expected 10 history entries, got %d", len(history))
	}
}
