package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/api"
	"github.com/swarm-emotions/orchestrator/internal/config"
	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()

	cacheClient := cache.NewMockClient()
	dbClient := db.NewMockClient()
	emotionClient := emotion.NewMockClient()
	vectorStoreClient := vectorstore.NewMockClient()
	llmProvider := llm.NewMockProvider()
	classifierClient := classifier.NewMockClient()

	orchestrator := pipeline.New(
		emotionClient,
		vectorStoreClient,
		cacheClient,
		dbClient,
		llmProvider,
		classifierClient,
	)
	handlers := api.NewHandlers(
		orchestrator,
		dbClient,
		cacheClient,
		cacheClient,
		dbClient,
		llmProvider,
		classifierClient,
	)

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           api.NewRouter(handlers),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("orchestrator starting", "port", cfg.HTTPPort)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
