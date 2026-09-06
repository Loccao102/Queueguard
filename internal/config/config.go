package config

import (
	"os"
	"strconv"
	"time"
)

type AppConfig struct {
	Port                string
	OriginURL           string
	SecretKey           string
	DischargeRatePerSec uint64
	TicketTTL           time.Duration
}

func Load() *AppConfig {
	port := getEnv("PORT", "8000")
	originURL := getEnv("ORIGIN_URL", "http://localhost:8080")
	secretKey := getEnv("SECRET_KEY", "queueguard-dev-secret-key-change-me")

	rateStr := getEnv("DISCHARGE_RATE", "10")
	rate, err := strconv.ParseUint(rateStr, 10, 64)
	if err != nil || rate == 0 {
		rate = 10
	}

	ttlStr := getEnv("TICKET_TTL", "10m")
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		ttl = 10 * time.Minute
	}

	return &AppConfig{
		Port:                port,
		OriginURL:           originURL,
		SecretKey:           secretKey,
		DischargeRatePerSec: rate,
		TicketTTL:           ttl,
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
