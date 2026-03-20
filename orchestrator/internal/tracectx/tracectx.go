package tracectx

import "context"

type contextKey struct{}

// WithTraceID stores the request trace ID in context so downstream connectors can
// propagate it without depending on HTTP-specific middleware packages.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, traceID)
}

func TraceID(ctx context.Context) string {
	traceID, _ := ctx.Value(contextKey{}).(string)
	return traceID
}

// Detach preserves trace correlation while dropping cancellation and deadline
// semantics for asynchronous background work.
func Detach(ctx context.Context) context.Context {
	detached := context.Background()
	if traceID := TraceID(ctx); traceID != "" {
		detached = WithTraceID(detached, traceID)
	}
	return detached
}
