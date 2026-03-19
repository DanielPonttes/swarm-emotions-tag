package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, message string) {
	respondJSON(w, status, errorResponse{
		Error:     message,
		RequestID: chimiddleware.GetReqID(r.Context()),
	})
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	f.Flush()
	return nil
}
