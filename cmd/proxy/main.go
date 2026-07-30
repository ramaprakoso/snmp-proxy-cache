package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"snmp-proxy-cache/internal/cache"
	"snmp-proxy-cache/internal/config"
	"snmp-proxy-cache/internal/proxy"
)

func main() {
	log.Println("==================================================================")
	log.Println("     Go SNMP Lazy Caching Proxy Service (Below OPNsense)          ")
	log.Println("==================================================================")

	// 1. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Failed to load config: %v", err)
	}

	log.Printf("[INFO] Loaded %d devices configuration from YAML/ENV\n", len(cfg.Devices))

	// 2. Initialize Redis Store
	redisStore, err := cache.NewRedisStore(cfg)
	if err != nil {
		log.Printf("[WARNING] Redis connection failed (%v). Operating with fallback mode.\n", err)
	} else {
		defer redisStore.Close()
		log.Println("[INFO] Connected to Redis Cache successfully.")
	}

	// 3. Initialize Lazy Handler & Proxy Server
	handler := proxy.NewLazyHandler(cfg, redisStore)
	server := proxy.NewServer(cfg, handler)

	// 4. Start UDP Listeners
	server.StartListeners()

	log.Println("[INFO] Go SNMP Proxy running. Press Ctrl+C to stop.")

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("[INFO] Shutting down Go SNMP Proxy gracefully.")
}
