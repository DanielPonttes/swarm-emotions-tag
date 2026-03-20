package tracectx

import (
	"context"
	"testing"
)

func TestDetachPreservesTraceID(t *testing.T) {
	ctx := WithTraceID(context.Background(), "req-123")

	detached := Detach(ctx)

	if got := TraceID(detached); got != "req-123" {
		t.Fatalf("expected trace ID to survive detach, got %q", got)
	}
}

func TestTraceIDUnsetByDefault(t *testing.T) {
	if got := TraceID(context.Background()); got != "" {
		t.Fatalf("expected empty trace ID, got %q", got)
	}
}
