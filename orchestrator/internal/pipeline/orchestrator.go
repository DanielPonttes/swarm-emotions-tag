package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

type Executor interface {
	Execute(ctx context.Context, input Input) (*Output, error)
}

type StreamExecutor interface {
	Executor
	ExecuteStream(ctx context.Context, input Input, callbacks StreamCallbacks) (*Output, error)
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

type StreamMetadata struct {
	NewEmotion   model.EmotionVector
	NewFsmState  model.FsmState
	NewIntensity float32
}

type StreamCallbacks struct {
	OnMetadata func(StreamMetadata) error
	OnChunk    func(string) error
}

type preparedGeneration struct {
	fsmResult        *FSMResult
	ranked           []model.RankedMemory
	cognitiveContext *model.CognitiveContext
	prompt           PromptPackage
	generateOpts     connector.GenerateOpts
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
		prepared, err := o.prepareGeneration(runCtx, input)
		if err != nil {
			return nil, err
		}

		o.observe("step7_generate")
		stepStart := time.Now()
		llmResponse, err := o.stepGenerate(runCtx, prepared.prompt.UserPrompt, prepared.generateOpts)
		o.metrics.ObserveStepDuration("step7_generate", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step7 generate: %w", err)
		}

		o.observe("step8_postprocess_dispatch")
		stepStart = time.Now()
		backgroundCtx := tracectx.Detach(runCtx)
		o.runBackground(func() {
			o.observe("step8_postprocess")
			backgroundStart := time.Now()
			o.stepPostProcess(
				backgroundCtx,
				input,
				llmResponse,
				prepared.fsmResult,
				prepared.ranked,
				prepared.cognitiveContext,
				prepared.prompt.Directive,
			)
			o.metrics.ObserveStepDuration("step8_postprocess", time.Since(backgroundStart))
		})
		o.metrics.ObserveStepDuration("step8_postprocess_dispatch", time.Since(stepStart))

		return &Output{
			LLMResponse:  llmResponse,
			NewEmotion:   prepared.fsmResult.NewEmotion,
			NewFsmState:  prepared.fsmResult.NewFsmState,
			NewIntensity: prepared.fsmResult.NewIntensity,
			LatencyMs:    time.Since(start).Milliseconds(),
		}, nil
	})
}

func (o *Orchestrator) ExecuteStream(ctx context.Context, input Input, callbacks StreamCallbacks) (*Output, error) {
	start := time.Now()
	return o.withAgentLock(ctx, input.AgentID, func(runCtx context.Context) (*Output, error) {
		prepared, err := o.prepareGeneration(runCtx, input)
		if err != nil {
			return nil, err
		}

		if callbacks.OnMetadata != nil {
			if err := callbacks.OnMetadata(StreamMetadata{
				NewEmotion:   prepared.fsmResult.NewEmotion,
				NewFsmState:  prepared.fsmResult.NewFsmState,
				NewIntensity: prepared.fsmResult.NewIntensity,
			}); err != nil {
				return nil, err
			}
		}

		o.observe("step7_generate")
		stepStart := time.Now()
		llmResponse, err := o.stepGenerateStream(runCtx, prepared.prompt.UserPrompt, prepared.generateOpts, callbacks.OnChunk)
		o.metrics.ObserveStepDuration("step7_generate", time.Since(stepStart))
		if err != nil {
			return nil, fmt.Errorf("step7 generate stream: %w", err)
		}

		o.observe("step8_postprocess_dispatch")
		stepStart = time.Now()
		backgroundCtx := tracectx.Detach(runCtx)
		o.runBackground(func() {
			o.observe("step8_postprocess")
			backgroundStart := time.Now()
			o.stepPostProcess(
				backgroundCtx,
				input,
				llmResponse,
				prepared.fsmResult,
				prepared.ranked,
				prepared.cognitiveContext,
				prepared.prompt.Directive,
			)
			o.metrics.ObserveStepDuration("step8_postprocess", time.Since(backgroundStart))
		})
		o.metrics.ObserveStepDuration("step8_postprocess_dispatch", time.Since(stepStart))

		return &Output{
			LLMResponse:  llmResponse,
			NewEmotion:   prepared.fsmResult.NewEmotion,
			NewFsmState:  prepared.fsmResult.NewFsmState,
			NewIntensity: prepared.fsmResult.NewIntensity,
			LatencyMs:    time.Since(start).Milliseconds(),
		}, nil
	})
}

func (o *Orchestrator) prepareGeneration(ctx context.Context, input Input) (*preparedGeneration, error) {
	o.observe("step1_load")
	stepStart := time.Now()
	agentState, err := o.cache.GetAgentState(ctx, input.AgentID)
	o.metrics.ObserveStepDuration("step1_load", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step1 get state: %w", err)
	}
	if agentState == nil {
		agentState = model.DefaultAgentState(input.AgentID)
	}

	stepStart = time.Now()
	agentConfig, err := o.db.GetAgentConfig(ctx, input.AgentID)
	o.metrics.ObserveStepDuration("step1_get_config", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step1 get config: %w", err)
	}
	if agentConfig == nil {
		stepStart = time.Now()
		agentConfig = model.DefaultAgentConfig(input.AgentID)
		if err := o.db.SaveAgentConfig(ctx, agentConfig); err != nil {
			o.metrics.ObserveStepDuration("step1_bootstrap_config", time.Since(stepStart))
			return nil, fmt.Errorf("step1 bootstrap config: %w", err)
		}
		o.metrics.ObserveStepDuration("step1_bootstrap_config", time.Since(stepStart))
	}

	o.observe("step2_perceive")
	stepStart = time.Now()
	stimulusVector, stimulusType, err := o.stepPerceive(ctx, input.Text)
	o.metrics.ObserveStepDuration("step2_perceive", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step2 perceive: %w", err)
	}

	o.observe("step3_fsm_vector")
	stepStart = time.Now()
	fsmResult, err := o.stepFSMAndVector(ctx, agentState, agentConfig, stimulusVector, stimulusType)
	o.metrics.ObserveStepDuration("step3_fsm_vector", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step3 fsm+vector: %w", err)
	}

	o.observe("step4_persist_state")
	stepStart = time.Now()
	if err := o.cache.SetAgentState(ctx, input.AgentID, &model.AgentState{
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
	candidates, cognitiveContext, workingMemory, err := o.stepRetrieve(ctx, input, fsmResult, agentConfig)
	o.metrics.ObserveStepDuration("step5_retrieve", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step5 retrieve: %w", err)
	}

	o.observe("step6_fuse")
	stepStart = time.Now()
	ranked, err := o.stepFuse(ctx, candidates, agentConfig, fsmResult.NewEmotion)
	o.metrics.ObserveStepDuration("step6_fuse", time.Since(stepStart))
	if err != nil {
		return nil, fmt.Errorf("step6 fuse: %w", err)
	}

	promptPackage := buildPromptPackage(
		input,
		ranked,
		fsmResult,
		cognitiveContext,
		workingMemory,
		o.generateOpts.MaxTokens,
		o.generateOpts.SystemPrompt,
	)
	generateOpts := o.generateOpts
	generateOpts.SystemPrompt = promptPackage.SystemPrompt

	return &preparedGeneration{
		fsmResult:        fsmResult,
		ranked:           ranked,
		cognitiveContext: cognitiveContext,
		prompt:           promptPackage,
		generateOpts:     generateOpts,
	}, nil
}

func (o *Orchestrator) stepGenerateStream(
	ctx context.Context,
	prompt string,
	opts connector.GenerateOpts,
	onChunk func(string) error,
) (string, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.45, 2*time.Second)
	defer cancel()

	streamingProvider, ok := o.llm.(connector.StreamingLLMProvider)
	if !ok {
		full, err := o.llm.Generate(stepCtx, prompt, opts)
		if err != nil {
			return "", err
		}
		for _, part := range splitStreamParts(full) {
			if onChunk == nil {
				continue
			}
			if err := onChunk(part); err != nil {
				return "", err
			}
		}
		return full, nil
	}

	ch, err := streamingProvider.GenerateStream(stepCtx, prompt, opts)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Done {
			break
		}
		if chunk.Text == "" {
			continue
		}
		builder.WriteString(chunk.Text)
		if onChunk != nil {
			if err := onChunk(chunk.Text); err != nil {
				return "", err
			}
		}
	}
	return builder.String(), nil
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

func splitStreamParts(text string) []string {
	const chunkSize = 48
	if strings.TrimSpace(text) == "" {
		return nil
	}

	runes := []rune(text)
	parts := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}
