package api

import (
	"context"
	"encoding/json"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

func (h *Handlers) Interact(w http.ResponseWriter, r *http.Request) {
	req, err := decodeInteractionRequest(r)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AgentID == "" || req.Text == "" {
		respondError(w, r, http.StatusBadRequest, "agent_id and text are required")
		return
	}

	result, err := h.executeInteraction(r.Context(), req)
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

func (h *Handlers) InteractStream(w http.ResponseWriter, r *http.Request) {
	req, err := decodeInteractionRequest(r)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" || req.Text == "" {
		respondError(w, r, http.StatusBadRequest, "agent_id and text are required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.respondInteractionJSON(w, r, req)
		return
	}

	streamer, ok := h.pipeline.(pipeline.StreamExecutor)
	if !ok {
		h.respondInteractionJSON(w, r, req)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	traceID := chimiddleware.GetReqID(r.Context())
	callbacks := pipeline.StreamCallbacks{
		OnMetadata: func(meta pipeline.StreamMetadata) error {
			return writeSSE(w, flusher, "metadata", map[string]any{
				"fsm_state": meta.NewFsmState.StateName,
				"emotion":   meta.NewEmotion,
				"intensity": meta.NewIntensity,
				"trace_id":  traceID,
				"agent_id":  req.AgentID,
				"streaming": true,
			})
		},
		OnChunk: func(text string) error {
			return writeSSE(w, flusher, "chunk", map[string]any{"text": text})
		},
	}

	result, err := streamer.ExecuteStream(r.Context(), interactionInput(req), callbacks)
	if err != nil {
		_ = writeSSE(w, flusher, "error", map[string]any{
			"error":      err.Error(),
			"request_id": traceID,
		})
		return
	}

	_ = writeSSE(w, flusher, "done", map[string]any{
		"status":     "complete",
		"latency_ms": result.LatencyMs,
	})
}

func (h *Handlers) respondInteractionJSON(w http.ResponseWriter, r *http.Request, req model.InteractionRequest) {
	result, err := h.executeInteraction(r.Context(), req)
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

func decodeInteractionRequest(r *http.Request) (model.InteractionRequest, error) {
	var req model.InteractionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func interactionInput(req model.InteractionRequest) pipeline.Input {
	return pipeline.Input{
		AgentID:  req.AgentID,
		Text:     req.Text,
		Metadata: req.Metadata,
	}
}

func (h *Handlers) executeInteraction(ctx context.Context, req model.InteractionRequest) (*pipeline.Output, error) {
	return h.pipeline.Execute(ctx, interactionInput(req))
}
