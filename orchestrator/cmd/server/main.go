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
		activeLLMProvider string
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
		activeLLMProvider = "mock"
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

		switch cfg.LLMProvider {
		case "mock":
			llmProvider = llm.NewMockProvider()
			activeLLMProvider = "mock"
		case "ollama-native":
			llmReal, err := llm.NewOllamaNativeProvider(llm.OllamaNativeConfig{
				BaseURL: cfg.LLMBaseURL,
			})
			if err != nil {
				slog.Error("init llm provider", "error", err)
				os.Exit(1)
			}
			llmProvider = llmReal
			activeLLMProvider = "ollama-native"
		case "openai-compatible":
			llmReal, err := llm.NewOpenAICompatibleProvider(llm.OpenAICompatibleConfig{
				BaseURL: cfg.LLMBaseURL,
				APIKey:  cfg.LLMAPIKey,
			})
			if err != nil {
				slog.Error("init llm provider", "error", err)
				os.Exit(1)
			}
			llmProvider = llmReal
			activeLLMProvider = "openai-compatible"
		default:
			slog.Error("unsupported llm provider", "provider", cfg.LLMProvider)
			os.Exit(1)
		}
		classifierReal := classifier.NewClient(cfg.PythonMLURL)
		classifierReal.SetMetricsReporter(metricsReporter)

		var classifierRuntime connector.ClassifierClient = classifierReal
		if cfg.ClassifierCacheEnabled {
			cachedClassifier := classifier.NewCachedClient(
				classifierRuntime,
				cfg.RedisAddr,
				time.Duration(cfg.ClassifierCacheTTLSeconds)*time.Second,
			)
			cachedClassifier.SetMetricsReporter(metricsReporter)
			classifierRuntime = cachedClassifier
			cleanups = append(cleanups, func() {
				if err := cachedClassifier.Close(); err != nil {
					slog.Warn("close classifier cache", "error", err)
				}
			})
		}
		if cfg.ClassifierFallbackNeutral {
			fallbackClassifier := classifier.NewFallbackClient(classifierRuntime)
			fallbackClassifier.SetMetricsReporter(metricsReporter)
			classifierRuntime = fallbackClassifier
		}
		classifierClient = classifierRuntime
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
	orchestrator.SetGenerateOpts(connector.GenerateOpts{
		Model:           cfg.LLMModel,
		SystemPrompt:    cfg.LLMSystemPrompt,
		MaxTokens:       cfg.LLMMaxTokens,
		Temperature:     cfg.LLMTemperature,
		TopP:            cfg.LLMTopP,
		TopK:            cfg.LLMTopK,
		PresencePenalty: cfg.LLMPresencePenalty,
		EnableThinking:  cfg.LLMEnableThinking,
	})
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

	slog.Info("orchestrator starting", "port", cfg.HTTPPort, "llm_provider", activeLLMProvider, "llm_model", cfg.LLMModel)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
