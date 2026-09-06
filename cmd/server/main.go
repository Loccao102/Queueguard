package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Loccao102/queueguard/internal/config"
	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/proxy"
	"github.com/Loccao102/queueguard/internal/queue"
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
	waitingRoom := queue.NewWaitingRoom(wrCfg, signer)

	// Start background token-bucket discharge worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go waitingRoom.StartDischargeWorker(ctx)

	// Initialize Reverse Proxy
	proxyServer, err := proxy.NewServer(cfg.OriginURL, waitingRoom, signer)
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
	log.Printf("🔍 Health Check URL: http://localhost:%s/queueguard/healthz", cfg.Port)
	log.Printf("📊 Live Status URL:  http://localhost:%s/queueguard/status", cfg.Port)

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
