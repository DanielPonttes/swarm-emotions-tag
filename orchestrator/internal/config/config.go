package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort          string
	EmotionEngineAddr string
	QdrantAddr        string
	QdrantCollection  string
	RedisAddr         string
	PostgresDSN       string
	PythonMLURL       string
	UseMockConnectors bool
	DefaultTimeoutSec int
}

func Load() Config {
	return Config{
		HTTPPort:          getEnv("HTTP_PORT", "8080"),
		EmotionEngineAddr: getEnv("EMOTION_ENGINE_ADDR", "localhost:50051"),
		QdrantAddr:        getEnv("QDRANT_ADDR", "localhost:6333"),
		QdrantCollection:  getEnv("QDRANT_COLLECTION", "memories"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		PostgresDSN:       getEnv("POSTGRES_DSN", "postgres://emotionrag:dev_password_change_me@localhost:5432/emotionrag?sslmode=disable"),
		PythonMLURL:       getEnv("PYTHON_ML_URL", "http://localhost:8090"),
		UseMockConnectors: getEnvBool("USE_MOCK_CONNECTORS", false),
		DefaultTimeoutSec: getEnvInt("DEFAULT_TIMEOUT_SEC", 30),
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
