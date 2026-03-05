package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort          string
	EmotionEngineAddr string
	PythonMLURL       string
	DefaultTimeoutSec int
}

func Load() Config {
	return Config{
		HTTPPort:          getEnv("HTTP_PORT", "8080"),
		EmotionEngineAddr: getEnv("EMOTION_ENGINE_ADDR", "localhost:50051"),
		PythonMLURL:       getEnv("PYTHON_ML_URL", "http://localhost:8090"),
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
