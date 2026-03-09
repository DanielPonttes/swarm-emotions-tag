package pipeline

import (
	"context"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func (o *Orchestrator) stepGenerate(
	ctx context.Context,
	input Input,
	ranked []model.RankedMemory,
	fsmResult *FSMResult,
	cognitive *model.CognitiveContext,
) (string, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.45, 2*time.Second)
	defer cancel()

	prompt := buildPrompt(input, ranked, fsmResult, cognitive)
	return o.llm.Generate(stepCtx, prompt, o.generateOpts)
}
