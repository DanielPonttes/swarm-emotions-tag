package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort                  string
	EmotionEngineAddr         string
	QdrantAddr                string
	QdrantCollection          string
	RedisAddr                 string
	PostgresDSN               string
	PythonMLURL               string
	ClassifierCacheEnabled    bool
	ClassifierCacheTTLSeconds int
	ClassifierFallbackNeutral bool
	LLMProvider               string
	LLMBaseURL                string
	LLMAPIKey                 string
	LLMModel                  string
	LLMSystemPrompt           string
	LLMMaxTokens              int
	LLMTemperature            float32
	LLMTopP                   float32
	LLMTopK                   int
	LLMPresencePenalty        float32
	LLMEnableThinking         bool
	UseMockConnectors         bool
	DefaultTimeoutSec         int
}

func Load() Config {
	return Config{
		HTTPPort:                  getEnv("HTTP_PORT", "8080"),
		EmotionEngineAddr:         getEnv("EMOTION_ENGINE_ADDR", "localhost:50051"),
		QdrantAddr:                getEnv("QDRANT_ADDR", "localhost:6333"),
		QdrantCollection:          getEnv("QDRANT_COLLECTION", "memories"),
		RedisAddr:                 getEnv("REDIS_ADDR", "localhost:6379"),
		PostgresDSN:               getEnv("POSTGRES_DSN", "postgres://emotionrag:dev_password_change_me@localhost:5433/emotionrag?sslmode=disable"),
		PythonMLURL:               getEnv("PYTHON_ML_URL", "http://localhost:8090"),
		ClassifierCacheEnabled:    getEnvBool("CLASSIFIER_CACHE_ENABLED", true),
		ClassifierCacheTTLSeconds: getEnvInt("CLASSIFIER_CACHE_TTL_SEC", 21600),
		ClassifierFallbackNeutral: getEnvBool("CLASSIFIER_FALLBACK_NEUTRAL", true),
		LLMProvider:               getEnv("LLM_PROVIDER", "mock"),
		LLMBaseURL:                getEnv("LLM_BASE_URL", "http://127.0.0.1:8000/v1"),
		LLMAPIKey:                 getEnv("LLM_API_KEY", ""),
		LLMModel:                  getEnv("LLM_MODEL", "Qwen/Qwen3.5-27B"),
		LLMSystemPrompt:           getEnv("LLM_SYSTEM_PROMPT", "You are a concise and emotionally coherent assistant. Reply briefly, helpfully, and stay aligned with the provided emotional context."),
		LLMMaxTokens:              getEnvInt("LLM_MAX_TOKENS", 256),
		LLMTemperature:            getEnvFloat32("LLM_TEMPERATURE", 0.2),
		LLMTopP:                   getEnvFloat32("LLM_TOP_P", 0.8),
		LLMTopK:                   getEnvInt("LLM_TOP_K", 20),
		LLMPresencePenalty:        getEnvFloat32("LLM_PRESENCE_PENALTY", 0.0),
		LLMEnableThinking:         getEnvBool("LLM_ENABLE_THINKING", false),
		UseMockConnectors:         getEnvBool("USE_MOCK_CONNECTORS", false),
		DefaultTimeoutSec:         getEnvInt("DEFAULT_TIMEOUT_SEC", 30),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvFloat32(key string, fallback float32) float32 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return fallback
	}
	return float32(parsed)
}
