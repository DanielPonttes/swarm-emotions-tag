package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/api"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           api.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("orchestrator starting", "port", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

