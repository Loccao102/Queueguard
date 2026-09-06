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
	EventStartTime      time.Time
	RedisURL            string
	BindDevice          bool
	PoWDifficulty       int // 0 = disabled, 3-5 = active difficulty
	TemplatePath        string
	EventTitle          string
	BrandLogoURL        string
	ThemeColor          string
	Announcement        string
	RoomsConfig         string // format: "vip:/tickets/vip:5,general:/tickets:30"
}

// RoomSpec holds parsed config for a room from ROOMS_CONFIG
type RoomSpec struct {
	ID         string
	PathPrefix string
	Rate       uint64
}

// ParseRooms parses the ROOMS_CONFIG string into a list of RoomSpec
func (c *AppConfig) ParseRooms() []RoomSpec {
	if c.RoomsConfig == "" {
		return nil
	}
	var specs []RoomSpec
	parts := strings.Split(c.RoomsConfig, ",")
	for _, part := range parts {
		tokens := strings.Split(strings.TrimSpace(part), ":")
		if len(tokens) >= 2 {
			id := strings.TrimSpace(tokens[0])
			prefix := strings.TrimSpace(tokens[1])
			var rate uint64 = c.DischargeRatePerSec
			if len(tokens) >= 3 {
				if r, err := strconv.ParseUint(tokens[2], 10, 64); err == nil && r > 0 {
					rate = r
				}
			}
			specs = append(specs, RoomSpec{
				ID:         id,
				PathPrefix: prefix,
				Rate:       rate,
			})
		}
	}
	return specs
}

func Load() *AppConfig {
	port := getEnv("PORT", "8000")
	originURL := getEnv("ORIGIN_URL", "http://localhost:8080")
	secretKey := getEnv("SECRET_KEY", "queueguard-dev-secret-key-change-me")
	adminToken := getEnv("ADMIN_TOKEN", "queueguard-admin-secret")
	redisURL := getEnv("REDIS_URL", "")

	var eventStartTime time.Time
	if timeStr := getEnv("EVENT_START_TIME", ""); timeStr != "" {
		if parsed, err := time.Parse(time.RFC3339, timeStr); err == nil {
			eventStartTime = parsed
		}
	}

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

	bindDeviceStr := getEnv("BIND_DEVICE", "true")
	bindDevice := bindDeviceStr != "false" && bindDeviceStr != "0"

	powDifficulty := getEnvInt("POW_DIFFICULTY", 0)

	templatePath := getEnv("WAITING_ROOM_TEMPLATE_PATH", "")
	eventTitle := getEnv("EVENT_TITLE", "Bạn Đang Trong Hàng Chờ")
	brandLogoURL := getEnv("BRAND_LOGO_URL", "")
	themeColor := getEnv("THEME_COLOR", "#6366f1")
	announcement := getEnv("ANNOUNCEMENT_TEXT", "")
	roomsConfig := getEnv("ROOMS_CONFIG", "")

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
		EventStartTime:      eventStartTime,
		RedisURL:            redisURL,
		BindDevice:          bindDevice,
		PoWDifficulty:       powDifficulty,
		TemplatePath:        templatePath,
		EventTitle:          eventTitle,
		BrandLogoURL:        brandLogoURL,
		ThemeColor:          themeColor,
		Announcement:        announcement,
		RoomsConfig:         roomsConfig,
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
