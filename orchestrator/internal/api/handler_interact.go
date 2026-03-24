package api

import (
	"context"
	"encoding/json"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/logctx"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
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

	traceID := chimiddleware.GetReqID(r.Context())
	ctx := tracectx.WithTraceID(r.Context(), traceID)
	result, err := h.executeInteraction(ctx, req)
	if err != nil {
		if connector.IsDependencyUnavailable(err) {
			logctx.Warn(ctx, "interaction dependency unavailable", "agent_id", req.AgentID, "error", err)
			respondError(w, r, http.StatusServiceUnavailable, connector.ErrDependencyUnavailable.Error())
			return
		}
		logctx.Error(ctx, "interaction failed", "agent_id", req.AgentID, "error", err)
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, model.InteractionResponse{
		Response:     result.LLMResponse,
		EmotionState: result.NewEmotion,
		FsmState:     result.NewFsmState,
		Intensity:    result.NewIntensity,
		LatencyMs:    result.LatencyMs,
		TraceID:      traceID,
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
	streamCtx := tracectx.WithTraceID(r.Context(), traceID)
	callbacks := pipeline.StreamCallbacks{
		OnMetadata: func(meta pipeline.StreamMetadata) error {
			return writeSSE(w, flusher, "metadata", map[string]any{
				"fsm_state": meta.NewFsmState,
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

	result, err := streamer.ExecuteStream(streamCtx, interactionInput(req), callbacks)
	if err != nil {
		logctx.Error(streamCtx, "interaction stream failed", "agent_id", req.AgentID, "error", err)
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
	traceID := chimiddleware.GetReqID(r.Context())
	ctx := tracectx.WithTraceID(r.Context(), traceID)
	result, err := h.executeInteraction(ctx, req)
	if err != nil {
		if connector.IsDependencyUnavailable(err) {
			logctx.Warn(ctx, "interaction fallback dependency unavailable", "agent_id", req.AgentID, "error", err)
			respondError(w, r, http.StatusServiceUnavailable, connector.ErrDependencyUnavailable.Error())
			return
		}
		logctx.Error(ctx, "interaction fallback failed", "agent_id", req.AgentID, "error", err)
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, model.InteractionResponse{
		Response:     result.LLMResponse,
		EmotionState: result.NewEmotion,
		FsmState:     result.NewFsmState,
		Intensity:    result.NewIntensity,
		LatencyMs:    result.LatencyMs,
		TraceID:      traceID,
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
