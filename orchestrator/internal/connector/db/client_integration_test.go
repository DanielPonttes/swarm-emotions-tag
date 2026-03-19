//go:build integration

package db_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/testutil"
)

func TestClientIntegration_CRUDAndHistory(t *testing.T) {
	dsn := testutil.EnvOrDefault(
		"POSTGRES_DSN",
		"postgres://emotionrag:dev_password_change_me@127.0.0.1:5433/emotionrag?sslmode=disable",
	)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	hostPort := parsed.Host
	if hostPort == "" {
		t.Fatalf("invalid postgres host in dsn: %q", dsn)
	}
	testutil.RequireTCP(t, hostPort)

	client, err := db.NewClient(dsn)
	if err != nil {
		t.Fatalf("new postgres client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	agentID := testutil.UniqueID("it-db")

	cfg := model.DefaultAgentConfig(agentID)
	cfg.DisplayName = "Integration Agent"
	if err := client.SaveAgentConfig(ctx, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	gotCfg, err := client.GetAgentConfig(ctx, agentID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if gotCfg == nil {
		t.Fatalf("expected config, got nil")
	}
	if gotCfg.DisplayName != "Integration Agent" {
		t.Fatalf("expected display name Integration Agent, got %q", gotCfg.DisplayName)
	}

	all, err := client.ListAgentConfigs(ctx)
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	found := false
	for _, item := range all {
		if item.AgentID == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find agent %s in list", agentID)
	}

	cognitive := &model.CognitiveContext{
		AgentID:        agentID,
		Goals:          []string{"validate integration"},
		Constraints:    []string{"no hallucination"},
		WorkingSummary: "summary-it",
		UpdatedAtMs:    time.Now().UnixMilli(),
	}
	if err := client.UpdateCognitiveContext(ctx, agentID, cognitive); err != nil {
		t.Fatalf("update cognitive context: %v", err)
	}
	gotCog, err := client.GetCognitiveContext(ctx, agentID)
	if err != nil {
		t.Fatalf("get cognitive context: %v", err)
	}
	if gotCog.WorkingSummary != "summary-it" {
		t.Fatalf("expected working summary summary-it, got %q", gotCog.WorkingSummary)
	}
	if len(gotCog.Goals) == 0 || gotCog.Goals[0] != "validate integration" {
		t.Fatalf("unexpected goals: %#v", gotCog.Goals)
	}

	now := time.Now().UnixMilli()
	if err := client.LogInteraction(ctx, &model.InteractionLog{
		AgentID:      agentID,
		UserText:     "hello",
		ResponseText: "hi there",
		Stimulus:     "praise",
		FsmState:     model.FsmState{StateName: "joyful", MacroState: "positive", EnteredAt: now},
		Emotion:      model.EmotionVector{Components: []float32{0.5, 0.2, 0.1, 0.0, 0.1, 0.0}},
		Intensity:    0.6,
		CreatedAtMs:  now,
	}); err != nil {
		t.Fatalf("log interaction: %v", err)
	}

	logs, err := client.GetInteractionLogs(ctx, agentID)
	if err != nil {
		t.Fatalf("get interaction logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected interaction logs for %s", agentID)
	}

	if err := client.AppendEmotionHistory(ctx, &model.EmotionHistoryEntry{
		AgentID:     agentID,
		FsmState:    model.FsmState{StateName: "joyful"},
		Emotion:     model.EmotionVector{Components: []float32{0.5, 0.2, 0.1, 0, 0, 0}},
		Intensity:   0.6,
		CreatedAtMs: now,
	}); err != nil {
		t.Fatalf("append emotion history: %v", err)
	}

	history, err := client.GetEmotionHistory(ctx, agentID)
	if err != nil {
		t.Fatalf("get emotion history: %v", err)
	}
	if len(history) == 0 {
		t.Fatalf("expected emotion history for %s", agentID)
	}

	if err := client.DeleteAgentConfig(ctx, agentID); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	gotAfterDelete, err := client.GetAgentConfig(ctx, agentID)
	if err != nil {
		t.Fatalf("get config after delete: %v", err)
	}
	if gotAfterDelete != nil {
		t.Fatalf("expected nil config after delete")
	}
}
