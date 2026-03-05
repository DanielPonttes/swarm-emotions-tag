package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestExecuteRunsStepsInOrder(t *testing.T) {
	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	if err := dbClient.SaveAgentConfig(context.Background(), model.DefaultAgentConfig("agent-1")); err != nil {
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

	var steps []string
	orchestrator.SetStepObserver(func(step string) {
		steps = append(steps, step)
	})

	output, err := orchestrator.Execute(context.Background(), Input{
		AgentID: "agent-1",
		Text:    "thanks for the great help",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.LLMResponse == "" {
		t.Fatalf("expected llm response")
	}

	expected := []string{
		"step1_load",
		"step2_perceive",
		"step3_fsm_vector",
		"step4_persist_state",
		"step5_retrieve",
		"step6_fuse",
		"step7_generate",
		"step8_postprocess_dispatch",
		"step8_postprocess",
	}
	if len(steps) != len(expected) {
		t.Fatalf("expected %d steps, got %d: %#v", len(expected), len(steps), steps)
	}
	for idx, step := range expected {
		if steps[idx] != step {
			t.Fatalf("expected step %d to be %s, got %s", idx, step, steps[idx])
		}
	}
}

func TestStepRetrieveRunsQueriesInParallel(t *testing.T) {
	orchestrator := New(
		emotion.NewMockClient(),
		slowVectorStore{delay: 75 * time.Millisecond},
		cache.NewMockClient(),
		&slowDB{MockClient: db.NewMockClient(), delay: 75 * time.Millisecond},
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)

	start := time.Now()
	_, _, err := orchestrator.stepRetrieve(context.Background(), Input{
		AgentID: "agent-2",
		Text:    "deadline update",
	}, &FSMResult{
		NewEmotion: model.EmotionVector{Components: []float32{0.1, 0.2, 0.3, 0.4, 0.2, 0.1}},
	}, model.DefaultAgentConfig("agent-2"))
	if err != nil {
		t.Fatalf("step retrieve: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed >= 180*time.Millisecond {
		t.Fatalf("expected parallel execution, took %s", elapsed)
	}
}

func TestExecutePropagatesErrors(t *testing.T) {
	orchestrator := New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cache.NewMockClient(),
		db.NewMockClient(),
		llm.NewMockProvider(),
		classifierErrorClient{},
	)

	_, err := orchestrator.Execute(context.Background(), Input{AgentID: "agent-3", Text: "test"})
	if err == nil || !errors.Is(err, errClassifierBoom) {
		t.Fatalf("expected classifier error, got %v", err)
	}
}

func TestWithStepTimeoutUsesMinimum(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	child, childCancel := withStepTimeout(ctx, 0.1, 20*time.Millisecond)
	defer childCancel()

	deadline, ok := child.Deadline()
	if !ok {
		t.Fatalf("expected child deadline")
	}
	if remaining := time.Until(deadline); remaining < 15*time.Millisecond {
		t.Fatalf("expected minimum timeout to apply, got %s", remaining)
	}
}

func TestPostProcessRunsInBackground(t *testing.T) {
	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	done := make(chan struct{}, 1)
	orchestrator := New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cacheClient,
		dbClient,
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)

	orchestrator.SetStepObserver(func(step string) {
		if step == "step8_postprocess" {
			done <- struct{}{}
		}
	})

	_, err := orchestrator.Execute(context.Background(), Input{AgentID: "agent-4", Text: "thanks"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("background postprocess did not run")
	}
}

type slowVectorStore struct {
	delay time.Duration
}

func (s slowVectorStore) QuerySemantic(_ context.Context, _ connector.QuerySemanticParams) ([]model.MemoryHit, error) {
	time.Sleep(s.delay)
	return []model.MemoryHit{{MemoryID: "semantic-1", Content: "semantic", SemanticScore: 0.8}}, nil
}

func (s slowVectorStore) QueryEmotional(_ context.Context, _ connector.QueryEmotionalParams) ([]model.MemoryHit, error) {
	time.Sleep(s.delay)
	return []model.MemoryHit{{MemoryID: "emotional-1", Content: "emotional", EmotionalScore: 0.7}}, nil
}

type slowDB struct {
	*db.MockClient
	delay time.Duration
}

func (s *slowDB) GetCognitiveContext(ctx context.Context, agentID string) (*model.CognitiveContext, error) {
	time.Sleep(s.delay)
	return s.MockClient.GetCognitiveContext(ctx, agentID)
}

var errClassifierBoom = errors.New("classifier boom")

type classifierErrorClient struct{}

func (classifierErrorClient) Ready(context.Context) error { return nil }

func (classifierErrorClient) ClassifyEmotion(context.Context, string) (*connector.EmotionClassification, error) {
	return nil, errClassifierBoom
}
