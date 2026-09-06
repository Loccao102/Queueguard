package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type AppConfig struct {
	Port                string
	OriginURL           string
	SecretKey           string
	DischargeRatePerSec uint64
	TicketTTL           time.Duration
	AdminToken          string
	BypassPaths         []string
	IPRateLimit         int // requests per minute
	IPRateBurst         int // maximum burst allowance
}

func Load() *AppConfig {
	port := getEnv("PORT", "8000")
	originURL := getEnv("ORIGIN_URL", "http://localhost:8080")
	secretKey := getEnv("SECRET_KEY", "queueguard-dev-secret-key-change-me")
	adminToken := getEnv("ADMIN_TOKEN", "queueguard-admin-secret")

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

	bypassStr := getEnv("BYPASS_PATHS", "")
	var bypassPaths []string
	if bypassStr != "" {
		for _, p := range strings.Split(bypassStr, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				bypassPaths = append(bypassPaths, trimmed)
			}
		}
	}

	ipLimit := getEnvInt("IP_RATE_LIMIT", 60)
	ipBurst := getEnvInt("IP_RATE_BURST", 20)

	return &AppConfig{
		Port:                port,
		OriginURL:           originURL,
		SecretKey:           secretKey,
		DischargeRatePerSec: rate,
		TicketTTL:           ttl,
		AdminToken:          adminToken,
		BypassPaths:         bypassPaths,
		IPRateLimit:         ipLimit,
		IPRateBurst:         ipBurst,
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
