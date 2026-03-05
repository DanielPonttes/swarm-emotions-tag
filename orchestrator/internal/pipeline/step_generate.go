package pipeline

import (
	"context"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func (o *Orchestrator) stepGenerate(
	ctx context.Context,
	input Input,
	ranked []model.RankedMemory,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
) (string, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.3, 500000000)
	defer cancel()

	prompt := buildPrompt(input, ranked, fsmResult, cognitive)
	return o.llm.Generate(stepCtx, prompt, connector.GenerateOpts{
		Model:       "mock-llm",
		MaxTokens:   256,
		Temperature: 0.2,
	})
}
