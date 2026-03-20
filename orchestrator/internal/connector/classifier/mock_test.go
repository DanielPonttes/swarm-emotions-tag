package classifier

import (
	"context"
	"testing"
)

func TestMockClientSupportsExpandedStimuli(t *testing.T) {
	client := NewMockClient()

	testCases := []struct {
		name     string
		text     string
		expected string
	}{
		{name: "resolution", text: "The issue is fixed now and resolved", expected: "resolution"},
		{name: "success", text: "It worked, success confirmed", expected: "success"},
		{name: "empathy", text: "I understand this is hard, sorry this happened", expected: "empathy"},
		{name: "user frustration", text: "I'm frustrated and stuck again", expected: "user_frustration"},
		{name: "boredom", text: "This is getting boring and repetitive", expected: "boredom"},
		{name: "severe criticism", text: "This rollout is unacceptable and terrible", expected: "severe_criticism"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := client.ClassifyEmotion(context.Background(), tc.text)
			if err != nil {
				t.Fatalf("classify emotion: %v", err)
			}
			if result.Stimulus != tc.expected {
				t.Fatalf("expected stimulus %q, got %q", tc.expected, result.Stimulus)
			}
		})
	}
}
