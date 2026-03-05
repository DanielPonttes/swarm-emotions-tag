package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if body := strings.TrimSpace(rec.Body.String()); body != `{"status":"ok"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestInteractStubRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interact", nil)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected status %d, got %d", http.StatusNotImplemented, rec.Code)
	}
}

