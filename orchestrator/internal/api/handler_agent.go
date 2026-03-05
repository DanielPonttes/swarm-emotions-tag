package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type agentUpsertRequest struct {
	AgentID     string `json:"agent_id"`
	DisplayName string `json:"display_name"`
}

func (h *Handlers) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req agentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		respondError(w, r, http.StatusBadRequest, "agent_id is required")
		return
	}

	cfg := model.DefaultAgentConfig(req.AgentID)
	if req.DisplayName != "" {
		cfg.DisplayName = req.DisplayName
	}
	if err := h.db.SaveAgentConfig(r.Context(), cfg); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = h.cache.SetAgentState(r.Context(), req.AgentID, model.DefaultAgentState(req.AgentID))
	respondJSON(w, http.StatusCreated, cfg)
}

func (h *Handlers) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.db.ListAgentConfigs(r.Context())
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (h *Handlers) GetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	cfg, err := h.db.GetAgentConfig(r.Context(), agentID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg == nil {
		respondError(w, r, http.StatusNotFound, "agent not found")
		return
	}
	respondJSON(w, http.StatusOK, cfg)
}

func (h *Handlers) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	cfg, err := h.db.GetAgentConfig(r.Context(), agentID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg == nil {
		respondError(w, r, http.StatusNotFound, "agent not found")
		return
	}

	var req agentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName != "" {
		cfg.DisplayName = req.DisplayName
	}
	if err := h.db.SaveAgentConfig(r.Context(), cfg); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, cfg)
}

func (h *Handlers) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	if err := h.db.DeleteAgentConfig(r.Context(), agentID); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
