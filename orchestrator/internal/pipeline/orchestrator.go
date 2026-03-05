package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type Executor interface {
	Execute(ctx context.Context, input Input) (*Output, error)
}

type Input struct {
	AgentID  string
	Text     string
	Metadata map[string]any
}

type Output struct {
	LLMResponse  string
	NewEmotion   model.EmotionVector
	NewFsmState  model.FsmState
	NewIntensity float32
	LatencyMs    int64
}

type Orchestrator struct {
	emotionClient connector.EmotionEngineClient
	vectorStore   connector.VectorStoreClient
	cache         connector.CacheClient
	db            connector.DBClient
	llm           connector.LLMProvider
	classifier    connector.ClassifierClient
	onStep        func(string)
	runBackground func(func())
}

func New(
	emotionClient connector.EmotionEngineClient,
	vectorStore connector.VectorStoreClient,
	cache connector.CacheClient,
	db connector.DBClient,
	llm connector.LLMProvider,
	classifier connector.ClassifierClient,
) *Orchestrator {
	return &Orchestrator{
		emotionClient: emotionClient,
		vectorStore:   vectorStore,
		cache:         cache,
		db:            db,
		llm:           llm,
		classifier:    classifier,
		runBackground: func(fn func()) { go fn() },
	}
}

func (o *Orchestrator) SetStepObserver(fn func(string)) {
	o.onStep = fn
}

func (o *Orchestrator) SetBackgroundRunner(fn func(func())) {
	o.runBackground = fn
}

func (o *Orchestrator) Execute(ctx context.Context, input Input) (*Output, error) {
	start := time.Now()
	return o.withAgentLock(ctx, input.AgentID, func(runCtx context.Context) (*Output, error) {
		o.observe("step1_load")
		agentState, err := o.cache.GetAgentState(runCtx, input.AgentID)
		if err != nil {
			return nil, fmt.Errorf("step1 get state: %w", err)
		}
		if agentState == nil {
			agentState = model.DefaultAgentState(input.AgentID)
		}

		agentConfig, err := o.db.GetAgentConfig(runCtx, input.AgentID)
		if err != nil {
			return nil, fmt.Errorf("step1 get config: %w", err)
		}
		if agentConfig == nil {
			agentConfig = model.DefaultAgentConfig(input.AgentID)
			if err := o.db.SaveAgentConfig(runCtx, agentConfig); err != nil {
				return nil, fmt.Errorf("step1 bootstrap config: %w", err)
			}
		}

		o.observe("step2_perceive")
		stimulusVector, stimulusType, err := o.stepPerceive(runCtx, input.Text)
		if err != nil {
			return nil, fmt.Errorf("step2 perceive: %w", err)
		}

		o.observe("step3_fsm_vector")
		fsmResult, err := o.stepFSMAndVector(runCtx, agentState, agentConfig, stimulusVector, stimulusType)
		if err != nil {
			return nil, fmt.Errorf("step3 fsm+vector: %w", err)
		}

		o.observe("step4_persist_state")
		if err := o.cache.SetAgentState(runCtx, input.AgentID, &model.AgentState{
			AgentID:         input.AgentID,
			CurrentEmotion:  fsmResult.NewEmotion,
			CurrentFsmState: fsmResult.NewFsmState,
			UpdatedAtMs:     time.Now().UnixMilli(),
		}); err != nil {
			return nil, fmt.Errorf("step4 update state: %w", err)
		}

		o.observe("step5_retrieve")
		candidates, cognitiveContext, err := o.stepRetrieve(runCtx, input, fsmResult, agentConfig)
		if err != nil {
			return nil, fmt.Errorf("step5 retrieve: %w", err)
		}

		o.observe("step6_fuse")
		ranked, err := o.stepFuse(runCtx, candidates, agentConfig, fsmResult.NewEmotion)
		if err != nil {
			return nil, fmt.Errorf("step6 fuse: %w", err)
		}

		o.observe("step7_generate")
		llmResponse, err := o.stepGenerate(runCtx, input, ranked, fsmResult, cognitiveContext)
		if err != nil {
			return nil, fmt.Errorf("step7 generate: %w", err)
		}

		o.observe("step8_postprocess_dispatch")
		o.runBackground(func() {
			o.observe("step8_postprocess")
			o.stepPostProcess(context.Background(), input, llmResponse, fsmResult, ranked)
		})

		return &Output{
			LLMResponse:  llmResponse,
			NewEmotion:   fsmResult.NewEmotion,
			NewFsmState:  fsmResult.NewFsmState,
			NewIntensity: fsmResult.NewIntensity,
			LatencyMs:    time.Since(start).Milliseconds(),
		}, nil
	})
}

func (o *Orchestrator) withAgentLock(
	ctx context.Context,
	agentID string,
	fn func(context.Context) (*Output, error),
) (*Output, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	for {
		locked, err := o.cache.AcquireAgentLock(ctx, agentID, 2*time.Second)
		if err != nil {
			return nil, fmt.Errorf("acquire agent lock: %w", err)
		}
		if locked {
			defer func() {
				_ = o.cache.ReleaseAgentLock(context.Background(), agentID)
			}()
			return fn(ctx)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for agent lock: %w", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (o *Orchestrator) observe(step string) {
	if o.onStep != nil {
		o.onStep(step)
	}
}

func timeoutBudget(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 30 * time.Second
	}
	return time.Until(deadline)
}

func withStepTimeout(ctx context.Context, fraction float64, minimum time.Duration) (context.Context, context.CancelFunc) {
	budget := timeoutBudget(ctx)
	stepTimeout := time.Duration(float64(budget) * fraction)
	if stepTimeout < minimum {
		stepTimeout = minimum
	}
	return context.WithTimeout(ctx, stepTimeout)
}
