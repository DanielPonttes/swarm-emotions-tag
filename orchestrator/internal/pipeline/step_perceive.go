package pipeline

import "context"

func (o *Orchestrator) stepPerceive(ctx context.Context, text string) (stimulusVector []float32, stimulusType string, err error) {
	stepCtx, cancel := withStepTimeout(ctx, 0.1, 250000000)
	defer cancel()

	classification, err := o.classifier.ClassifyEmotion(stepCtx, text)
	if err != nil {
		return nil, "", err
	}
	return classification.Vector.Components, classification.Stimulus, nil
}
