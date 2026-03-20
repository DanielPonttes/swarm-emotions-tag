package api

import (
	"net/http"
	"net/http/pprof"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(defaultHTTPTimeout))

	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
	r.Handle("/metrics", promhttp.Handler())
	r.Route("/debug/pprof", func(r chi.Router) {
		r.Get("/", pprof.Index)
		r.Get("/cmdline", pprof.Cmdline)
		r.Get("/profile", pprof.Profile)
		r.Get("/symbol", pprof.Symbol)
		r.Post("/symbol", pprof.Symbol)
		r.Get("/trace", pprof.Trace)
		r.Get("/allocs", pprof.Handler("allocs").ServeHTTP)
		r.Get("/block", pprof.Handler("block").ServeHTTP)
		r.Get("/goroutine", pprof.Handler("goroutine").ServeHTTP)
		r.Get("/heap", pprof.Handler("heap").ServeHTTP)
		r.Get("/mutex", pprof.Handler("mutex").ServeHTTP)
		r.Get("/threadcreate", pprof.Handler("threadcreate").ServeHTTP)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/interact", h.Interact)
		r.Post("/interact/stream", h.InteractStream)
		r.Route("/agents", func(r chi.Router) {
			r.Post("/", h.CreateAgent)
			r.Get("/", h.ListAgents)
			r.Route("/{agentID}", func(r chi.Router) {
				r.Get("/", h.GetAgent)
				r.Put("/", h.UpdateAgent)
				r.Delete("/", h.DeleteAgent)
				r.Get("/state", h.GetAgentState)
				r.Get("/history", h.GetEmotionHistory)
			})
		})
	})

	return r
}
