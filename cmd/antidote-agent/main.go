package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/connection"
	"github.com/codebasehealth/antidote-agent/internal/health"
	"github.com/codebasehealth/antidote-agent/internal/router"
)

var (
	token       = flag.String("token", "", "Agent token (or ANTIDOTE_TOKEN env)")
	endpoint    = flag.String("endpoint", "", "WebSocket endpoint (or ANTIDOTE_ENDPOINT env)")
	showVersion = flag.Bool("version", false, "Show version and exit")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("antidote-agent version %s\n", connection.Version)
		os.Exit(0)
	}

	// Get token from flag or env
	agentToken := *token
	if agentToken == "" {
		agentToken = os.Getenv("ANTIDOTE_TOKEN")
	}
	if agentToken == "" {
		log.Fatal("Token required: use --token flag or ANTIDOTE_TOKEN env")
	}

	// Get endpoint from flag or env
	agentEndpoint := *endpoint
	if agentEndpoint == "" {
		agentEndpoint = os.Getenv("ANTIDOTE_ENDPOINT")
	}
	if agentEndpoint == "" {
		agentEndpoint = "wss://antidote.codebasehealth.com/agent/ws"
	}

	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting antidote-agent...")
	log.Printf("Endpoint: %s", agentEndpoint)

	// Create context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create connection manager
	var msgRouter *router.Router
	connMgr := connection.NewManager(agentToken, agentEndpoint, func(msgType string, data []byte) {
		if msgRouter != nil {
			msgRouter.Handle(msgType, data)
		}
	})

	// Create router (needs connection manager's send function)
	msgRouter = router.NewRouter(connMgr.Send)

	// Create health monitor
	healthMon := health.NewMonitor(connMgr.Send)

	// Start connection manager
	if err := connMgr.Start(ctx); err != nil {
		log.Fatalf("Failed to start connection manager: %v", err)
	}

	// Start health monitor (every 60 seconds)
	healthMon.Start(ctx, 60*time.Second)

	// Wait for connection
	log.Println("Connecting to server...")

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")

	// Cancel context to stop all goroutines
	cancel()

	// Stop components
	healthMon.Stop()
	connMgr.Stop()

	log.Println("Shutdown complete")
}
