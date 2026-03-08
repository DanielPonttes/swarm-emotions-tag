package api

import (
	"encoding/json"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

func (h *Handlers) Interact(w http.ResponseWriter, r *http.Request) {
	var req model.InteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AgentID == "" || req.Text == "" {
		respondError(w, r, http.StatusBadRequest, "agent_id and text are required")
		return
	}

	result, err := h.pipeline.Execute(r.Context(), pipeline.Input{
		AgentID:  req.AgentID,
		Text:     req.Text,
		Metadata: req.Metadata,
	})
	if err != nil {
		if connector.IsDependencyUnavailable(err) {
			respondError(w, r, http.StatusServiceUnavailable, connector.ErrDependencyUnavailable.Error())
			return
		}
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, model.InteractionResponse{
		Response:     result.LLMResponse,
		EmotionState: result.NewEmotion,
		FsmState:     result.NewFsmState.StateName,
		Intensity:    result.NewIntensity,
		LatencyMs:    result.LatencyMs,
		TraceID:      chimiddleware.GetReqID(r.Context()),
	})
}
