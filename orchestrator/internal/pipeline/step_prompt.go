package pipeline

import (
	"fmt"
	"strings"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

func buildPrompt(input Input, ranked []model.RankedMemory, fsmResult *FSMResult, cognitive *model.CognitiveContext) string {
	var memories []string
	for _, item := range ranked {
		memories = append(memories, item.Content)
		if len(memories) == 3 {
			break
		}
	}

	return fmt.Sprintf(
		"AgentID: %s\nUser: %s\nFSM: %s\nIntensity: %.3f\nGoals: %s\nContext: %s\nRelevant memories: %s\nRespond briefly and helpfully.",
		input.AgentID,
		input.Text,
		fsmResult.NewFsmState.StateName,
		fsmResult.NewIntensity,
		strings.Join(cognitive.Goals, ", "),
		cognitive.WorkingSummary,
		strings.Join(memories, " | "),
	)
}
