package router

import (
	"log"

	"github.com/codebasehealth/antidote-agent/internal/discovery"
	"github.com/codebasehealth/antidote-agent/internal/executor"
	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/codebasehealth/antidote-agent/internal/security"
)

// SendFunc is a function that sends a message
type SendFunc func(msg interface{}) error

// Router routes incoming messages to appropriate handlers
type Router struct {
	executor  *executor.Executor
	validator *security.Validator
	send      SendFunc
}

// NewRouter creates a new message router
func NewRouter(send SendFunc) *Router {
	r := &Router{
		send:      send,
		validator: security.NewValidator(),
	}

	// Create executor with output/complete/rejected handlers and security validator
	r.executor = executor.New(
		r.handleOutput,
		r.handleComplete,
		r.handleRejected,
		r.validator,
	)

	return r
}

// Handle processes an incoming message
func (r *Router) Handle(msgType string, data []byte) {
	switch msgType {
	case messages.TypeCommand:
		r.handleCommand(data)
	case messages.TypeDiscover:
		r.handleDiscover()
	case messages.TypeAuthOK, messages.TypeAuthError:
		// Already handled by connection manager
	default:
		log.Printf("Unhandled message type: %s", msgType)
	}
}

// handleCommand processes a command message
func (r *Router) handleCommand(data []byte) {
	cmdMsg, err := messages.ParseCommandMessage(data)
	if err != nil {
		log.Printf("Failed to parse command message: %v", err)
		return
	}

	log.Printf("Received command %s: %s", cmdMsg.ID, cmdMsg.Command)

	if err := r.executor.Execute(cmdMsg); err != nil {
		log.Printf("Failed to execute command: %v", err)
	}
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

// Executor returns the executor
func (r *Router) Executor() *executor.Executor {
	return r.executor
}
