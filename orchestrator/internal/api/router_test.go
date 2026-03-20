package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

func newTestRouter() http.Handler {
	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	orchestrator := pipeline.New(
		emotion.NewMockClient(),
		vectorstore.NewMockClient(),
		cacheClient,
		dbClient,
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)
	orchestrator.SetBackgroundRunner(func(fn func()) { fn() })
	handlers := NewHandlers(
		orchestrator,
		dbClient,
		cacheClient,
		cacheClient,
		dbClient,
		llm.NewMockProvider(),
		classifier.NewMockClient(),
	)
	return NewRouter(handlers)
}

func TestHealthAndReadyRoutes(t *testing.T) {
	router := newTestRouter()

	for _, path := range []string{"/health", "/ready", "/metrics", "/debug/pprof/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, rec.Code)
		}
	}
}

func TestAgentLifecycleAndStateRoutes(t *testing.T) {
	router := newTestRouter()

	createBody := bytes.NewBufferString(`{"agent_id":"agent-1","display_name":"Agent One"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create agent expected 201, got %d", createRec.Code)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-1/state", nil)
	stateRec := httptest.NewRecorder()
	router.ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("get state expected 200, got %d", stateRec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list agents expected 200, got %d", listRec.Code)
	}
}

func TestInteractAndHistoryRoutes(t *testing.T) {
	router := newTestRouter()

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/", bytes.NewBufferString(`{"agent_id":"agent-42"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create agent expected 201, got %d", createRec.Code)
	}

	body := bytes.NewBufferString(`{"agent_id":"agent-42","text":"thanks for the great help"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/interact", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("interact expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interact response: %v", err)
	}
	if payload["trace_id"] == "" {
		t.Fatalf("expected trace_id in interact response")
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-42/history", nil)
	historyRec := httptest.NewRecorder()
	router.ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history expected 200, got %d", historyRec.Code)
	}
}

func TestInteractStreamRoute(t *testing.T) {
	router := newTestRouter()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/interact/stream",
		bytes.NewBufferString(`{"agent_id":"agent-stream","text":"need help with git push"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("interact stream expected 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", contentType)
	}

	body := rec.Body.String()
	for _, expected := range []string{"event: metadata", "event: chunk", "event: done"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected stream body to contain %q\n%s", expected, body)
		}
	}
}

func TestRequestLoggerIncludesTraceID(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	router := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health expected 200, got %d", rec.Code)
	}

	output := logs.String()
	if !strings.Contains(output, "trace_id=") {
		t.Fatalf("expected request log to include trace_id, got %q", output)
	}
	if !strings.Contains(output, "path=/health") {
		t.Fatalf("expected request log to include path, got %q", output)
	}
	if !strings.Contains(output, "status=200") {
		t.Fatalf("expected request log to include status, got %q", output)
	}
}
