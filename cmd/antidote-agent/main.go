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

	"github.com/codebasehealth/antidote-agent/internal/config"
	"github.com/codebasehealth/antidote-agent/internal/connection"
	"github.com/codebasehealth/antidote-agent/internal/health"
	"github.com/codebasehealth/antidote-agent/internal/router"
)

var (
	configPath  = flag.String("config", "", "Path to config file")
	token       = flag.String("token", "", "Agent token (overrides config)")
	endpoint    = flag.String("endpoint", "", "WebSocket endpoint (overrides config)")
	showVersion = flag.Bool("version", false, "Show version and exit")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("antidote-agent version %s\n", connection.Version)
		os.Exit(0)
	}

	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("Starting antidote-agent...")

	// Load config
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Apply command line overrides
	if *token != "" {
		cfg.Connection.Token = *token
	}
	if *endpoint != "" {
		cfg.Connection.Endpoint = *endpoint
	}

	log.Printf("Server: %s (%s)", cfg.Server.Name, cfg.Server.Environment)
	log.Printf("Endpoint: %s", cfg.Connection.Endpoint)
	log.Printf("Actions: %v", cfg.GetActionNames())

	// Create context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create connection manager
	var msgRouter *router.Router
	connMgr := connection.NewManager(cfg, func(msgType string, data []byte) {
		if msgRouter != nil {
			msgRouter.Handle(msgType, data)
		}
	})

	// Create router (needs connection manager's send function)
	msgRouter = router.NewRouter(cfg, connMgr.Send)

	// Create health monitor
	healthMon := health.NewMonitor(cfg, connMgr.Send)

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

func loadConfig() (*config.Config, error) {
	path := *configPath

	if path == "" {
		var err error
		path, err = config.FindConfigFile()
		if err != nil {
			// No config file found, check for required env vars
			if os.Getenv("ANTIDOTE_TOKEN") == "" || os.Getenv("ANTIDOTE_ENDPOINT") == "" {
				return nil, fmt.Errorf("no config file found and ANTIDOTE_TOKEN/ANTIDOTE_ENDPOINT not set")
			}

			// Create minimal config from env vars
			return &config.Config{
				Server: config.ServerConfig{
					Name:        os.Getenv("ANTIDOTE_SERVER_NAME"),
					Environment: os.Getenv("ANTIDOTE_ENVIRONMENT"),
				},
				Connection: config.ConnectionConfig{
					Endpoint:  os.Getenv("ANTIDOTE_ENDPOINT"),
					Token:     os.Getenv("ANTIDOTE_TOKEN"),
					Heartbeat: 30 * time.Second,
					Reconnect: config.ReconnectConfig{
						InitialDelay: 1 * time.Second,
						MaxDelay:     30 * time.Second,
						Multiplier:   2.0,
					},
				},
				Actions: make(map[string]config.Action),
			}, nil
		}
	}

	return config.Load(path)
}
