package router

import (
	"encoding/json"
	"log"

	"github.com/codebasehealth/antidote-agent/internal/discovery"
	"github.com/codebasehealth/antidote-agent/internal/executor"
	"github.com/codebasehealth/antidote-agent/internal/logmonitor"
	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/codebasehealth/antidote-agent/internal/security"
	"github.com/codebasehealth/antidote-agent/internal/signing"
)

// SendFunc is a function that sends a message
type SendFunc func(msg interface{}) error

// Router routes incoming messages to appropriate handlers
type Router struct {
	executor          *executor.Executor
	validator         *security.Validator
	verifier          *signing.Verifier
	logMonitor        *logmonitor.Monitor
	discoveryProvider *discoveryProvider
	send              SendFunc
}

// discoveryProvider implements logmonitor.AppDiscovery
type discoveryProvider struct {
	apps []messages.AppInfo
}

func (p *discoveryProvider) GetApps() []messages.AppInfo {
	return p.apps
}

// NewRouter creates a new message router
func NewRouter(send SendFunc, publicKey string) *Router {
	r := &Router{
		send:      send,
		validator: security.NewValidator(),
	}

	// Initialize signature verifier
	var err error
	r.verifier, err = signing.NewVerifier(publicKey)
	if err != nil {
		log.Printf("Warning: Failed to initialize signature verifier: %v", err)
		log.Printf("Message signing verification is DISABLED")
	} else if r.verifier.IsEnabled() {
		log.Printf("Message signing verification is ENABLED")
	} else {
		log.Printf("Message signing verification is DISABLED (no public key configured)")
	}

	// Create executor with output/complete/rejected handlers and security validator
	r.executor = executor.New(
		r.handleOutput,
		r.handleComplete,
		r.handleRejected,
		r.validator,
	)

	// Create discovery provider and log monitor
	r.discoveryProvider = &discoveryProvider{}
	r.logMonitor = logmonitor.NewMonitor(logmonitor.SendFunc(send), r.discoveryProvider)
	r.logMonitor.Start()

	return r
}

// Handle processes an incoming message
func (r *Router) Handle(msgType string, data []byte) {
	switch msgType {
	case messages.TypeCommand:
		r.handleCommand(data)
	case messages.TypeDiscover:
		r.handleDiscover()
	case messages.TypeMonitoringConfig:
		r.handleMonitoringConfig(data)
	case messages.TypeAuthOK, messages.TypeAuthError:
		// Already handled by connection manager
	default:
		log.Printf("Unhandled message type: %s", msgType)
	}
}

// handleCommand processes a command message
func (r *Router) handleCommand(data []byte) {
	// Verify signature if verifier is enabled
	if r.verifier != nil && r.verifier.IsEnabled() {
		signedCmd, err := r.verifier.VerifyCommand(data)
		if err != nil {
			log.Printf("SECURITY: Command signature verification failed: %v", err)

			// Try to extract command ID for rejection message
			cmdID := extractCommandID(data)
			if cmdID != "" {
				r.handleRejected(messages.NewRejectedMessage(
					cmdID,
					"SIGNATURE_INVALID",
					err.Error(),
				))
			}
			return
		}

		log.Printf("Command %s signature verified", signedCmd.ID)

		// Convert SignedCommand to CommandMessage
		cmdMsg := &messages.CommandMessage{
			Type:       signedCmd.Type,
			ID:         signedCmd.ID,
			Command:    signedCmd.Command,
			WorkingDir: signedCmd.WorkingDir,
			Env:        signedCmd.Env,
			Timeout:    signedCmd.Timeout,
		}

		log.Printf("Received command %s: %s", cmdMsg.ID, cmdMsg.Command)

		if err := r.executor.Execute(cmdMsg); err != nil {
			log.Printf("Failed to execute command: %v", err)
		}
		return
	}

	// No signature verification - parse normally
	cmdMsg, err := messages.ParseCommandMessage(data)
	if err != nil {
		log.Printf("Failed to parse command message: %v", err)
		return
	}

	log.Printf("Received command %s: %s (unsigned)", cmdMsg.ID, cmdMsg.Command)

	if err := r.executor.Execute(cmdMsg); err != nil {
		log.Printf("Failed to execute command: %v", err)
	}
}

// extractCommandID tries to extract the command ID from raw JSON data
func extractCommandID(data []byte) string {
	// Simple extraction for rejection messages
	type idOnly struct {
		ID string `json:"id"`
	}
	var msg idOnly
	if err := json.Unmarshal(data, &msg); err != nil {
		return ""
	}
	return msg.ID
}

// handleDiscover runs server discovery and sends results
func (r *Router) handleDiscover() {
	log.Printf("Running server discovery...")

	discoveryMsg := discovery.Discover()

	// Update security validator with discovered apps
	if r.validator != nil && len(discoveryMsg.Apps) > 0 {
		r.validator.UpdateApps(discoveryMsg.Apps)
		log.Printf("Security validator updated with %d apps", len(discoveryMsg.Apps))
	}

	// Update discovery provider for log monitor
	if r.discoveryProvider != nil {
		r.discoveryProvider.apps = discoveryMsg.Apps
		log.Printf("Discovery provider updated with %d apps", len(discoveryMsg.Apps))
	}

	if err := r.send(discoveryMsg); err != nil {
		log.Printf("Failed to send discovery: %v", err)
	} else {
		log.Printf("Discovery sent: %d services, %d languages, %d apps",
			len(discoveryMsg.Services),
			len(discoveryMsg.Languages),
			len(discoveryMsg.Apps))
	}
}

// handleOutput sends command output to the cloud
func (r *Router) handleOutput(msg *messages.OutputMessage) {
	if err := r.send(msg); err != nil {
		log.Printf("Failed to send output: %v", err)
	}
}

// handleComplete sends command completion to the cloud
func (r *Router) handleComplete(msg *messages.CompleteMessage) {
	if err := r.send(msg); err != nil {
		log.Printf("Failed to send complete: %v", err)
	}
}

// handleRejected sends command rejection to the cloud
func (r *Router) handleRejected(msg *messages.RejectedMessage) {
	log.Printf("Command %s rejected: [%s] %s", msg.ID, msg.Code, msg.Message)
	if err := r.send(msg); err != nil {
		log.Printf("Failed to send rejected: %v", err)
	}
}

// handleMonitoringConfig processes monitoring configuration from the cloud
func (r *Router) handleMonitoringConfig(data []byte) {
	configMsg, err := messages.ParseMonitoringConfigMessage(data)
	if err != nil {
		log.Printf("Failed to parse monitoring config: %v", err)
		return
	}

	log.Printf("Received monitoring config with %d apps", len(configMsg.Apps))

	if r.logMonitor != nil {
		r.logMonitor.UpdateConfig(configMsg)
	}
}

// Executor returns the executor
func (r *Router) Executor() *executor.Executor {
	return r.executor
}

// LogMonitor returns the log monitor
func (r *Router) LogMonitor() *logmonitor.Monitor {
	return r.logMonitor
}

// Stop stops the router and its components
func (r *Router) Stop() {
	if r.logMonitor != nil {
		r.logMonitor.Stop()
	}
}
