package classifier

import (
	"context"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
)

var neutralEmotionVector = []float32{0.0, 0.0, 0.0, 0.5, 0.0, 0.0}

type FallbackClient struct {
	inner   connector.ClassifierClient
	metrics observability.Reporter
}

func NewFallbackClient(inner connector.ClassifierClient) *FallbackClient {
	return &FallbackClient{
		inner:   inner,
		metrics: observability.NewNoopReporter(),
	}
}

func (c *FallbackClient) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *FallbackClient) Ready(ctx context.Context) error {
	return c.inner.Ready(ctx)
}

func (c *FallbackClient) ClassifyEmotion(ctx context.Context, text string) (*connector.EmotionClassification, error) {
	metrics := c.metrics
	if metrics == nil {
		metrics = observability.NewNoopReporter()
	}

	result, err := c.inner.ClassifyEmotion(ctx, text)
	if err == nil {
		return result, nil
	}

	metrics.IncDependencyError("classifier", "fallback_neutral")
	return &connector.EmotionClassification{
		Vector:     model.EmotionVector{Components: append([]float32(nil), neutralEmotionVector...)},
		Label:      "neutral",
		Confidence: 0.0,
		Stimulus:   inferStimulus(text, "neutral"),
	}, nil
}
