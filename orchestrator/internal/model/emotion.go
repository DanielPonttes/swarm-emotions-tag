package model

import "math"

type EmotionVector struct {
	Components []float32 `json:"components"`
}

func (v EmotionVector) Clone() EmotionVector {
	out := make([]float32, len(v.Components))
	copy(out, v.Components)
	return EmotionVector{Components: out}
}

func (v EmotionVector) Intensity() float32 {
	var sum float32
	for _, c := range v.Components {
		sum += c * c
	}
	return float32(math.Sqrt(float64(sum)))
}

func (v EmotionVector) Dimension() int {
	return len(v.Components)
}

type FsmState struct {
	StateName  string `json:"state_name"`
	MacroState string `json:"macro_state"`
	EnteredAt  int64  `json:"entered_at_ms"`
}
