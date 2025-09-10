package main

import (
	"os"
	"time"
)

// Config holds application configuration
type Config struct {
	DatabaseURL                  string
	ServerAddress                string
	ListenerMinReconnectInterval time.Duration
	ListenerMaxReconnectInterval time.Duration
}

// NewConfig creates a new configuration with defaults
func NewConfig() *Config {
	return &Config{
		DatabaseURL:                  getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost/postgres?sslmode=disable"),
		ServerAddress:                getEnv("SERVER_ADDRESS", ":8080"),
		ListenerMinReconnectInterval: 10 * time.Second,
		ListenerMaxReconnectInterval: time.Minute,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
