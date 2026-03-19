package pipeline

import (
	"context"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
)

func (o *Orchestrator) stepGenerate(
	ctx context.Context,
	prompt string,
	opts connector.GenerateOpts,
) (string, error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.45, 2*time.Second)
	defer cancel()

	return o.llm.Generate(stepCtx, prompt, opts)
}
