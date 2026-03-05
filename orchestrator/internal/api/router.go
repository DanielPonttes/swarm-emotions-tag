package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(defaultHTTPTimeout))

	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/interact", h.Interact)
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
