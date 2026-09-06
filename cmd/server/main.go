package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Loccao102/queueguard/internal/config"
	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/proxy"
	"github.com/Loccao102/queueguard/internal/queue"
	"github.com/Loccao102/queueguard/internal/ratelimit"
)

const banner = "===================================================================\n" +
	"   QUEUEGUARD: Virtual Waiting Room & Traffic Shaper Reverse Proxy\n" +
	"==================================================================="

func main() {
	fmt.Println(banner)

	cfg := config.Load()

	// Initialize Ticket Signer (HMAC-SHA256)
	signer := crypto.NewSigner(cfg.SecretKey, cfg.TicketTTL)

	// Initialize Virtual Waiting Room engine
	wrCfg := queue.DefaultConfig()
	wrCfg.DischargeRatePerSec = cfg.DischargeRatePerSec
	wrCfg.TicketTTL = cfg.TicketTTL
	wrCfg.EventStartTime = cfg.EventStartTime

	var waitingRoom *queue.WaitingRoom
	var distributedEngine *queue.RedisEngine
	if cfg.RedisURL != "" {
		var err error
		distributedEngine, err = queue.NewRedisEngine(cfg.RedisURL, "queueguard")
		if err != nil {
			log.Fatalf("failed to connect to Redis: %v", err)
		}
		defer distributedEngine.Close()
		waitingRoom = queue.NewWaitingRoomWithEngine(wrCfg, signer, distributedEngine)
	} else {
		waitingRoom = queue.NewWaitingRoom(wrCfg, signer)
	}

	// Initialize IP Rate Limiter
	ipLimiter := ratelimit.NewIPRateLimiter(cfg.IPRateLimit, cfg.IPRateBurst)
	defer ipLimiter.Close()

	// Start background token-bucket discharge worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go waitingRoom.StartDischargeWorker(ctx)

	// Initialize RoomManager for multi-room routing
	roomManager := queue.NewRoomManager(waitingRoom)
	if specs := cfg.ParseRooms(); len(specs) > 0 {
		log.Printf("🎪 Multi-Waiting Room Mode detected: %d custom room(s) configured", len(specs))
		for _, spec := range specs {
			rCfg := queue.DefaultConfig()
			rCfg.RoomID = spec.ID
			rCfg.Name = strings.ToUpper(spec.ID) + " Lounge"
			rCfg.DischargeRatePerSec = spec.Rate
			rCfg.TicketTTL = cfg.TicketTTL
			rCfg.EventStartTime = cfg.EventStartTime

			var rRoom *queue.WaitingRoom
			if distributedEngine != nil {
				rRoom = queue.NewWaitingRoomWithEngine(rCfg, signer, distributedEngine)
			} else {
				rRoom = queue.NewWaitingRoom(rCfg, signer)
			}
			go rRoom.StartDischargeWorker(ctx)

			_ = roomManager.Register(queue.RoomDefinition{
				ID:                  spec.ID,
				Name:                rCfg.Name,
				PathPrefix:          spec.PathPrefix,
				DischargeRatePerSec: spec.Rate,
				TicketTTL:           cfg.TicketTTL,
				EventStartTime:      cfg.EventStartTime,
			}, rRoom)
			log.Printf("   ↳ Room '%s' -> prefix: %s | discharge: %d users/sec", spec.ID, spec.PathPrefix, spec.Rate)
		}
	}

	// Initialize Reverse Proxy
	proxyServer, err := proxy.NewServer(
		cfg.OriginURL,
		waitingRoom,
		signer,
		proxy.WithAdminToken(cfg.AdminToken),
		proxy.WithBypassPaths(cfg.BypassPaths),
		proxy.WithRateLimiter(ipLimiter),
		proxy.WithDeviceBinding(cfg.BindDevice),
		proxy.WithPoWDifficulty(cfg.PoWDifficulty),
		proxy.WithRoomManager(roomManager),
		proxy.WithTemplatePath(cfg.TemplatePath),
		proxy.WithBranding(cfg.EventTitle, cfg.BrandLogoURL, cfg.ThemeColor, cfg.Announcement),
	)
	if err != nil {
		log.Fatalf("❌ Failed to initialize QueueGuard proxy: %v", err)
	}

	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      proxyServer,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streaming requires no write timeout
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("🛡️  QueueGuard Reverse Proxy listening on :%s", cfg.Port)
	log.Printf("🎯 Forwarding admitted traffic to Origin: %s", cfg.OriginURL)
	log.Printf("⚡ Admission Discharge Rate: %d users/second", cfg.DischargeRatePerSec)
	log.Printf("🎟️  Ticket TTL Duration: %v", cfg.TicketTTL)
	log.Printf("🚦 IP Rate Limiter: %d req/min (burst %d)", cfg.IPRateLimit, cfg.IPRateBurst)
	log.Printf("🔍 Health Check URL:    http://localhost:%s/queueguard/healthz", cfg.Port)
	log.Printf("📊 Live Status URL:     http://localhost:%s/queueguard/status", cfg.Port)
	log.Printf("⚙️  Admin Dashboard URL: http://localhost:%s/queueguard/admin?token=%s", cfg.Port, cfg.AdminToken)
	log.Printf("📈 Prometheus Metrics:  http://localhost:%s/metrics", cfg.Port)

	// Graceful shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("\n⏳ Shutting down QueueGuard gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown warning: %v", err)
	}

	log.Println("✅ QueueGuard stopped cleanly.")
}
