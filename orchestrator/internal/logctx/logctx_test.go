package logctx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

func TestWarnIncludesTraceID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(previous)

	ctx := tracectx.WithTraceID(context.Background(), "req-123")
	Warn(ctx, "test warning", "step", "classify")

	output := logs.String()
	if !strings.Contains(output, "trace_id=req-123") {
		t.Fatalf("expected trace_id in log output, got %q", output)
	}
	if !strings.Contains(output, "step=classify") {
		t.Fatalf("expected custom attrs in log output, got %q", output)
	}
}
