package pipeline

import (
	"context"
	"time"
)

func (o *Orchestrator) stepGenerate(
	ctx context.Context,
	prompt string,
) (string, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.45, 2*time.Second)
	defer cancel()

	return o.llm.Generate(stepCtx, prompt, o.generateOpts)
}
