// Package config читает настройки сервера из переменных окружения.
package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	JWTSecret       string
	// Env: "development" (по умолчанию) или "production".
	Env string
	// CORSOrigins: адреса веб-клиентов через запятую (CORS_ORIGINS), например http://localhost:8081. Для мобильных приложений не нужно.
	CORSOrigins []string
	// ExpoAccessToken нужен, только если в проекте Expo включена защита push-токенов; ExpoPushURL менять не требуется.
	ExpoAccessToken string
	ExpoPushURL     string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ShutdownTimeout: 10 * time.Second,
		JWTSecret:       os.Getenv("JWT_SECRET"),
		ExpoAccessToken: os.Getenv("EXPO_ACCESS_TOKEN"),
		ExpoPushURL:     os.Getenv("EXPO_PUSH_URL"),
		Env:             getenv("APP_ENV", "development"),
		CORSOrigins:     splitList(os.Getenv("CORS_ORIGINS")),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET is required and must be at least 32 characters (e.g. `openssl rand -hex 32`)")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
