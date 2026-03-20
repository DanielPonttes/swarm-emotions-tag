package api

import (
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
)

type Handlers struct {
	pipeline pipeline.Executor
	db       connector.DBClient
	cache    connector.CacheClient
	ready    []connector.ReadyChecker
}

func NewHandlers(
	pipeline pipeline.Executor,
	db connector.DBClient,
	cache connector.CacheClient,
	ready ...connector.ReadyChecker,
) *Handlers {
	return &Handlers{
		pipeline: pipeline,
		db:       db,
		cache:    cache,
		ready:    ready,
	}
}

func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	ctx := tracectx.WithTraceID(r.Context(), chimiddleware.GetReqID(r.Context()))
	for _, dependency := range h.ready {
		if dependency == nil {
			continue
		}
		if err := dependency.Ready(ctx); err != nil {
			respondJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
