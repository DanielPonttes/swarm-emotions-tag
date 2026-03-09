package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
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
	generateOpts  connector.GenerateOpts
	metrics       observability.Reporter
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
		generateOpts: connector.GenerateOpts{
			Model:           "mock-llm",
			SystemPrompt:    "You are a concise and emotionally coherent assistant.",
			MaxTokens:       256,
			Temperature:     0.2,
			TopP:            0.8,
			TopK:            20,
			PresencePenalty: 0,
			EnableThinking:  false,
		},
		metrics:       observability.NewNoopReporter(),
		runBackground: func(fn func()) { go fn() },
	}
}

func (o *Orchestrator) SetStepObserver(fn func(string)) {
	o.onStep = fn
}

func (o *Orchestrator) SetBackgroundRunner(fn func(func())) {
	o.runBackground = fn
}

func (o *Orchestrator) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		o.metrics = observability.NewNoopReporter()
		return
	}
	o.metrics = reporter
}

func (o *Orchestrator) SetGenerateOpts(opts connector.GenerateOpts) {
	o.generateOpts = opts
}

func (o *Orchestrator) Execute(ctx context.Context, input Input) (*Output, error) {
	start := time.Now()
	return o.withAgentLock(ctx, input.AgentID, func(runCtx context.Context) (*Output, error) {
		o.observe("step1_load")
		stepStart := time.Now()
		agentState, err := o.cache.GetAgentState(runCtx, input.AgentID)
		o.metrics.ObserveStepDuration("step1_load", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step1 get state: %w", err)
		}
		if agentState == nil {
			agentState = model.DefaultAgentState(input.AgentID)
		}

		stepStart = time.Now()
		agentConfig, err := o.db.GetAgentConfig(runCtx, input.AgentID)
		o.metrics.ObserveStepDuration("step1_get_config", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step1 get config: %w", err)
		}
		if agentConfig == nil {
			stepStart = time.Now()
			agentConfig = model.DefaultAgentConfig(input.AgentID)
			if err := o.db.SaveAgentConfig(runCtx, agentConfig); err != nil {
				o.metrics.ObserveStepDuration("step1_bootstrap_config", time.Since(stepStart))
				return nil, fmt.Errorf("step1 bootstrap config: %w", err)
			}
			o.metrics.ObserveStepDuration("step1_bootstrap_config", time.Since(stepStart))
		}

		o.observe("step2_perceive")
		stepStart = time.Now()
		stimulusVector, stimulusType, err := o.stepPerceive(runCtx, input.Text)
		o.metrics.ObserveStepDuration("step2_perceive", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step2 perceive: %w", err)
		}

		o.observe("step3_fsm_vector")
		stepStart = time.Now()
		fsmResult, err := o.stepFSMAndVector(runCtx, agentState, agentConfig, stimulusVector, stimulusType)
		o.metrics.ObserveStepDuration("step3_fsm_vector", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step3 fsm+vector: %w", err)
		}

		o.observe("step4_persist_state")
		stepStart = time.Now()
		if err := o.cache.SetAgentState(runCtx, input.AgentID, &model.AgentState{
			AgentID:         input.AgentID,
			CurrentEmotion:  fsmResult.NewEmotion,
			CurrentFsmState: fsmResult.NewFsmState,
			UpdatedAtMs:     time.Now().UnixMilli(),
		}); err != nil {
			o.metrics.ObserveStepDuration("step4_persist_state", time.Since(stepStart))
			return nil, fmt.Errorf("step4 update state: %w", err)
		}
		o.metrics.ObserveStepDuration("step4_persist_state", time.Since(stepStart))

		o.observe("step5_retrieve")
		stepStart = time.Now()
		candidates, cognitiveContext, err := o.stepRetrieve(runCtx, input, fsmResult, agentConfig)
		o.metrics.ObserveStepDuration("step5_retrieve", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step5 retrieve: %w", err)
		}

		o.observe("step6_fuse")
		stepStart = time.Now()
		ranked, err := o.stepFuse(runCtx, candidates, agentConfig, fsmResult.NewEmotion)
		o.metrics.ObserveStepDuration("step6_fuse", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step6 fuse: %w", err)
		}

		o.observe("step7_generate")
		stepStart = time.Now()
		llmResponse, err := o.stepGenerate(runCtx, input, ranked, fsmResult, cognitiveContext)
		o.metrics.ObserveStepDuration("step7_generate", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step7 generate: %w", err)
		}

		o.observe("step8_postprocess_dispatch")
		stepStart = time.Now()
		o.runBackground(func() {
			o.observe("step8_postprocess")
			backgroundStart := time.Now()
			o.stepPostProcess(context.Background(), input, llmResponse, fsmResult, ranked)
			o.metrics.ObserveStepDuration("step8_postprocess", time.Since(backgroundStart))
		})
		o.metrics.ObserveStepDuration("step8_postprocess_dispatch", time.Since(stepStart))

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
