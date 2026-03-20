package api

import (
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/swarm-emotions/orchestrator/internal/logctx"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := chimiddleware.GetReqID(r.Context())
		ctx := tracectx.WithTraceID(r.Context(), traceID)
		r = r.WithContext(ctx)

		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		logctx.Info(ctx, "http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"latency_ms", time.Since(start).Milliseconds(),
		)
	})
}
