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
	"github.com/codebasehealth/antidote-agent/internal/updater"
)

var (
	token       = flag.String("token", "", "Agent token (or ANTIDOTE_TOKEN env)")
	endpoint    = flag.String("endpoint", "", "WebSocket endpoint (or ANTIDOTE_ENDPOINT env)")
	showVersion = flag.Bool("version", false, "Show version and exit")
	selfUpdate  = flag.Bool("self-update", false, "Update to the latest version")
	checkUpdate = flag.Bool("check-update", false, "Check if an update is available")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("antidote-agent version %s\n", connection.Version)
		os.Exit(0)
	}

	if *checkUpdate {
		result, err := updater.CheckForUpdate()
		if err != nil {
			fmt.Printf("Error checking for updates: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Current version: %s\n", result.CurrentVersion)
		fmt.Printf("Latest version:  %s\n", result.LatestVersion)
		if result.UpdateAvailable {
			fmt.Println("\nUpdate available! Run with --self-update to install.")
		} else {
			fmt.Println("\nYou're running the latest version.")
		}
		os.Exit(0)
	}

	if *selfUpdate {
		fmt.Printf("Current version: %s\n", connection.Version)
		fmt.Println("Checking for updates...")

		result, err := updater.SelfUpdate()
		if err != nil {
			fmt.Printf("Update failed: %v\n", err)
			os.Exit(1)
		}

		if !result.UpdateAvailable {
			fmt.Printf("Already running the latest version (%s)\n", result.CurrentVersion)
			os.Exit(0)
		}

		if result.Updated {
			fmt.Printf("Successfully updated to %s\n", result.LatestVersion)
			fmt.Println("\nRestart the service to use the new version:")
			fmt.Println("  sudo systemctl restart antidote-agent")
		}
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
