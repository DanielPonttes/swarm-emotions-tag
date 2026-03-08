package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/swarm-emotions/orchestrator/internal/api"
	"github.com/swarm-emotions/orchestrator/internal/config"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/connector/cache"
	"github.com/swarm-emotions/orchestrator/internal/connector/classifier"
	"github.com/swarm-emotions/orchestrator/internal/connector/db"
	"github.com/swarm-emotions/orchestrator/internal/connector/emotion"
	"github.com/swarm-emotions/orchestrator/internal/connector/llm"
	"github.com/swarm-emotions/orchestrator/internal/connector/vectorstore"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/pipeline"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	metricsReporter := observability.NewPrometheusReporter(prometheus.DefaultRegisterer)

	var (
		cacheClient       connector.CacheClient
		dbClient          connector.DBClient
		emotionClient     connector.EmotionEngineClient
		vectorStoreClient connector.VectorStoreClient
		llmProvider       connector.LLMProvider
		classifierClient  connector.ClassifierClient
		readyChecks       []connector.ReadyChecker
		cleanups          []func()
	)

	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	if cfg.UseMockConnectors {
		slog.Info("starting with mock connectors")
		cacheClient = cache.NewMockClient()
		dbClient = db.NewMockClient()
		emotionClient = emotion.NewMockClient()
		vectorStoreClient = vectorstore.NewMockClient()
		llmProvider = llm.NewMockProvider()
		classifierClient = classifier.NewMockClient()
	} else {
		slog.Info("starting with real connectors")

		cacheReal := cache.NewClient(cfg.RedisAddr)
		cacheReal.SetMetricsReporter(metricsReporter)
		cacheClient = cacheReal
		cleanups = append(cleanups, func() {
			if err := cacheReal.Close(); err != nil {
				slog.Warn("close redis client", "error", err)
			}
		})

		dbReal, err := db.NewClient(cfg.PostgresDSN)
		if err != nil {
			slog.Error("init postgres client", "error", err)
			os.Exit(1)
		}
		dbReal.SetMetricsReporter(metricsReporter)
		dbClient = dbReal
		cleanups = append(cleanups, dbReal.Close)

		emotionReal, err := emotion.NewClient(cfg.EmotionEngineAddr)
		if err != nil {
			slog.Error("init emotion client", "error", err)
			os.Exit(1)
		}
		emotionClient = emotion.NewCircuitBreakerClient(
			emotionReal,
			emotion.DefaultCircuitBreakerConfig(),
			metricsReporter,
		)
		cleanups = append(cleanups, func() {
			if err := emotionReal.Close(); err != nil {
				slog.Warn("close emotion client", "error", err)
			}
		})

		vectorReal, err := vectorstore.NewClient(cfg.QdrantAddr, cfg.QdrantCollection)
		if err != nil {
			slog.Error("init qdrant client", "error", err)
			os.Exit(1)
		}
		vectorReal.SetMetricsReporter(metricsReporter)
		vectorStoreClient = vectorReal

		llmProvider = llm.NewMockProvider()
		classifierReal := classifier.NewClient(cfg.PythonMLURL)
		classifierReal.SetMetricsReporter(metricsReporter)
		classifierClient = classifierReal
	}

	readyChecks = append(readyChecks, cacheClient, dbClient, llmProvider, classifierClient)
	if checker, ok := vectorStoreClient.(connector.ReadyChecker); ok {
		readyChecks = append(readyChecks, checker)
	}

	orchestrator := pipeline.New(
		emotionClient,
		vectorStoreClient,
		cacheClient,
		dbClient,
		llmProvider,
		classifierClient,
	)
	orchestrator.SetMetricsReporter(metricsReporter)
	handlers := api.NewHandlers(
		orchestrator,
		dbClient,
		cacheClient,
		readyChecks...,
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
