package pipeline

import (
	"math"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

type EmotionRegion struct {
	Name      string
	Center    [3]float32
	Radius    float32
	Directive string
	ToneHints []string
}

var emotionRegions = []EmotionRegion{
	{
		Name:      "panic",
		Center:    [3]float32{-0.7, 0.9, -0.4},
		Radius:    0.55,
		Directive: "Respond with urgency, directness, and immediate next actions. Avoid long digressions.",
		ToneHints: []string{"urgent", "direct", "stabilizing"},
	},
	{
		Name:      "joyful",
		Center:    [3]float32{0.8, 0.6, 0.4},
		Radius:    0.5,
		Directive: "Respond with constructive energy and a positive tone while staying grounded and useful.",
		ToneHints: []string{"upbeat", "encouraging", "clear"},
	},
	{
		Name:      "calm",
		Center:    [3]float32{0.2, -0.3, 0.5},
		Radius:    0.5,
		Directive: "Respond in a calm, analytic, and well-structured way. Favor evidence and clear sequencing.",
		ToneHints: []string{"measured", "analytic", "professional"},
	},
	{
		Name:      "empathetic",
		Center:    [3]float32{0.3, 0.1, -0.2},
		Radius:    0.45,
		Directive: "Acknowledge friction briefly, then guide with supportive and practical language.",
		ToneHints: []string{"supportive", "warm", "practical"},
	},
	{
		Name:      "frustrated",
		Center:    [3]float32{-0.5, 0.6, 0.3},
		Radius:    0.45,
		Directive: "Stay composed, cut ambiguity, and focus on the shortest path to resolution.",
		ToneHints: []string{"firm", "focused", "solution-oriented"},
	},
	{
		Name:      "curious",
		Center:    [3]float32{0.3, 0.35, 0.2},
		Radius:    0.45,
		Directive: "Be exploratory and insightful, but keep the response tethered to the user's objective.",
		ToneHints: []string{"curious", "thoughtful", "engaged"},
	},
	{
		Name:      "neutral",
		Center:    [3]float32{0, 0, 0},
		Radius:    1.2,
		Directive: "Maintain a neutral, concise, and reliable tone adapted to the task at hand.",
		ToneHints: []string{"neutral", "concise", "reliable"},
	},
}

func FindEmotionDirective(emotion model.EmotionVector) EmotionRegion {
	vad := [3]float32{
		emotionComponent(emotion, 0),
		emotionComponent(emotion, 1),
		emotionComponent(emotion, 2),
	}

	best := emotionRegions[len(emotionRegions)-1]
	bestDistance := float32(math.MaxFloat32)

	for _, region := range emotionRegions {
		distance := euclideanDistance3(vad, region.Center)
		if distance <= region.Radius && distance < bestDistance {
			best = region
			bestDistance = distance
		}
	}
	return best
}

func euclideanDistance3(a, b [3]float32) float32 {
	var sum float64
	for i := range a {
		diff := float64(a[i] - b[i])
		sum += diff * diff
	}
	return float32(math.Sqrt(sum))
}
