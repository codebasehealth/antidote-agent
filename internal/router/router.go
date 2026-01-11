package router

import (
	"log"

	"github.com/codebasehealth/antidote-agent/internal/config"
	"github.com/codebasehealth/antidote-agent/internal/executor"
	"github.com/codebasehealth/antidote-agent/internal/messages"
)

// SendFunc is a function that sends a message
type SendFunc func(msg interface{}) error

// Router routes incoming messages to appropriate handlers
type Router struct {
	cfg      *config.Config
	executor *executor.Pool
	send     SendFunc
}

// NewRouter creates a new message router
func NewRouter(cfg *config.Config, send SendFunc) *Router {
	r := &Router{
		cfg:  cfg,
		send: send,
	}

	// Create executor with output/complete handlers
	r.executor = executor.NewPool(
		cfg,
		r.handleCommandOutput,
		r.handleCommandComplete,
	)

	return r
}

// Handle processes an incoming message
func (r *Router) Handle(msgType string, data []byte) {
	switch msgType {
	case messages.TypeCommand:
		r.handleCommand(data)
	case messages.TypeLogRequest:
		r.handleLogRequest(data)
	case messages.TypeAuthResponse:
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

	log.Printf("Received command: %s (action: %s)", cmdMsg.ID, cmdMsg.Action)

	if err := r.executor.Execute(cmdMsg); err != nil {
		log.Printf("Failed to execute command: %v", err)
	}
}

// handleLogRequest processes a log request message
func (r *Router) handleLogRequest(data []byte) {
	logReq, err := messages.ParseLogRequestMessage(data)
	if err != nil {
		log.Printf("Failed to parse log request message: %v", err)
		return
	}

	log.Printf("Received log request: %s (paths: %v)", logReq.ID, logReq.Paths)

	// Gather logs (simplified implementation)
	entries := r.gatherLogs(logReq)

	// Send response
	response := messages.NewLogResponseMessage(logReq.ID, entries, len(entries) >= logReq.Limit)
	if err := r.send(response); err != nil {
		log.Printf("Failed to send log response: %v", err)
	}
}

// gatherLogs collects log entries from the specified paths
func (r *Router) gatherLogs(req *messages.LogRequestMessage) []messages.LogEntry {
	// Simplified implementation - in production, this would:
	// 1. Glob expand paths
	// 2. Read and filter log files
	// 3. Apply since/until filtering
	// 4. Apply regex filter
	// 5. Respect limit

	// For now, return empty entries
	// TODO: Implement proper log gathering
	return []messages.LogEntry{}
}

// handleCommandOutput sends command output to the server
func (r *Router) handleCommandOutput(msg *messages.CommandOutputMessage) {
	if err := r.send(msg); err != nil {
		log.Printf("Failed to send command output: %v", err)
	}
}

// handleCommandComplete sends command completion to the server
func (r *Router) handleCommandComplete(msg *messages.CommandCompleteMessage) {
	if err := r.send(msg); err != nil {
		log.Printf("Failed to send command complete: %v", err)
	}
}

// Executor returns the executor pool
func (r *Router) Executor() *executor.Pool {
	return r.executor
}
