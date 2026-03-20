package logctx

import (
	"context"
	"log/slog"

	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

func appendTraceAttrs(ctx context.Context, attrs []any) []any {
	traceID := tracectx.TraceID(ctx)
	if traceID == "" {
		return attrs
	}

	out := make([]any, 0, len(attrs)+2)
	out = append(out, "trace_id", traceID)
	out = append(out, attrs...)
	return out
}

func Info(ctx context.Context, msg string, attrs ...any) {
	slog.InfoContext(ctx, msg, appendTraceAttrs(ctx, attrs)...)
}

func Warn(ctx context.Context, msg string, attrs ...any) {
	slog.WarnContext(ctx, msg, appendTraceAttrs(ctx, attrs)...)
}

func Error(ctx context.Context, msg string, attrs ...any) {
	slog.ErrorContext(ctx, msg, appendTraceAttrs(ctx, attrs)...)
}
