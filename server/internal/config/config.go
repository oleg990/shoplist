// Package config читает настройки сервера из переменных окружения.
package config

import (
	"errors"
	"os"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	JWTSecret       string
	// Env: "development" (по умолчанию) или "production". В production обязателен SMTP.
	Env  string
	SMTP SMTP
	// ExpoAccessToken нужен, только если в проекте Expo включена защита push-токенов; ExpoPushURL менять не требуется.
	ExpoAccessToken string
	ExpoPushURL     string
}

// SMTP настройки отправки писем с кодами. Пустой Host означает «не настроено».
type SMTP struct {
	Host, Port, User, Password, From string
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
		SMTP: SMTP{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     getenv("SMTP_PORT", "587"),
			User:     os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     os.Getenv("SMTP_FROM"),
		},
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET is required and must be at least 32 characters (e.g. `openssl rand -hex 32`)")
	}
	if cfg.Env == "production" && (cfg.SMTP.Host == "" || cfg.SMTP.From == "") {
		return Config{}, errors.New("SMTP_HOST and SMTP_FROM are required when APP_ENV=production")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
