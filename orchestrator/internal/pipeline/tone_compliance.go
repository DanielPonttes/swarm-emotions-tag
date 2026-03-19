package pipeline

import (
	"log/slog"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/observability"
)

const lowToneComplianceThreshold = 0.40

func measureToneCompliance(
	directive EmotionRegion,
	responseEmotion *connector.EmotionClassification,
	metrics observability.Reporter,
) float64 {
	if responseEmotion == nil {
		return 0
	}

	actualVAD := [3]float32{
		emotionComponent(responseEmotion.Vector, 0),
		emotionComponent(responseEmotion.Vector, 1),
		emotionComponent(responseEmotion.Vector, 2),
	}
	score := toneComplianceScore(directive.Center, actualVAD)

	if metrics == nil {
		metrics = observability.NewNoopReporter()
	}
	metrics.ObserveToneCompliance(directive.Name, score)

	if score < lowToneComplianceThreshold {
		slog.Warn(
			"low tone compliance",
			"directive", directive.Name,
			"score", score,
			"desired_vad", directive.Center,
			"actual_vad", actualVAD,
		)
	}

	return score
}

func toneComplianceScore(desired, actual [3]float32) float64 {
	const maxDistance = 3.4641016151377544
	distance := float64(euclideanDistance3(desired, actual))
	score := 1 - (distance / maxDistance)
	switch {
	case score < 0:
		return 0
	case score > 1:
		return 1
	default:
		return score
	}
}
