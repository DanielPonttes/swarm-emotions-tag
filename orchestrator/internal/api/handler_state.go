package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func (h *Handlers) GetAgentState(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	state, err := h.cache.GetAgentState(r.Context(), agentID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if state == nil {
		state = model.DefaultAgentState(agentID)
	}
	respondJSON(w, http.StatusOK, state)
}

func (h *Handlers) GetEmotionHistory(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	history, err := h.db.GetEmotionHistory(r.Context(), agentID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (h *Handlers) GetInteractionLogs(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	logs, err := h.db.GetInteractionLogs(r.Context(), agentID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"interactions": logs})
}
