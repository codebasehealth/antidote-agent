package messages

import (
	"encoding/json"
	"time"
)

// Message types
const (
	TypeAuth            = "auth"
	TypeAuthResponse    = "auth_response"
	TypeHeartbeat       = "heartbeat"
	TypeCommand         = "command"
	TypeCommandOutput   = "command_output"
	TypeCommandComplete = "command_complete"
	TypeHealth          = "health"
	TypeLogRequest      = "log_request"
	TypeLogResponse     = "log_response"
)

// BaseMessage contains the type field common to all messages
type BaseMessage struct {
	Type string `json:"type"`
}

// ServerInfo contains information about the server/agent
type ServerInfo struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

// AuthMessage is sent by the agent to authenticate
type AuthMessage struct {
	Type         string     `json:"type"`
	Token        string     `json:"token"`
	AgentVersion string     `json:"agent_version"`
	Server       ServerInfo `json:"server"`
	Capabilities []string   `json:"capabilities"`
	Actions      []string   `json:"actions"`
}

// NewAuthMessage creates a new auth message
func NewAuthMessage(token, version string, server ServerInfo, capabilities, actions []string) *AuthMessage {
	return &AuthMessage{
		Type:         TypeAuth,
		Token:        token,
		AgentVersion: version,
		Server:       server,
		Capabilities: capabilities,
		Actions:      actions,
	}
}

// AuthResponseMessage is sent by the server after authentication
type AuthResponseMessage struct {
	Type      string `json:"type"`
	Status    string `json:"status"` // "ok" or "error"
	ServerID  string `json:"server_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// HeartbeatMessage is sent periodically to keep the connection alive
type HeartbeatMessage struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// NewHeartbeatMessage creates a new heartbeat message
func NewHeartbeatMessage() *HeartbeatMessage {
	return &HeartbeatMessage{
		Type:      TypeHeartbeat,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// CommandMessage is sent by the server to execute a command
type CommandMessage struct {
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Action       string            `json:"action"`
	Params       map[string]string `json:"params"`
	Timeout      int               `json:"timeout"`
	StreamOutput bool              `json:"stream_output"`
}

// CommandOutputMessage is sent by the agent during command execution
type CommandOutputMessage struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Stream    string `json:"stream"` // "stdout" or "stderr"
	Data      string `json:"data"`
	Timestamp string `json:"timestamp"`
}

// NewCommandOutputMessage creates a new command output message
func NewCommandOutputMessage(id, stream, data string) *CommandOutputMessage {
	return &CommandOutputMessage{
		Type:      TypeCommandOutput,
		ID:        id,
		Stream:    stream,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// CommandCompleteMessage is sent by the agent when a command finishes
type CommandCompleteMessage struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Timestamp  string `json:"timestamp"`
}

// NewCommandCompleteMessage creates a new command complete message
func NewCommandCompleteMessage(id string, exitCode int, durationMs int64) *CommandCompleteMessage {
	return &CommandCompleteMessage{
		Type:       TypeCommandComplete,
		ID:         id,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// HealthCheck represents a single health check result
type HealthCheck struct {
	Status    string `json:"status"` // "passing" or "failing"
	LatencyMs int    `json:"latency_ms"`
	LastCheck string `json:"last_check"`
}

// SystemMetrics contains system resource metrics
type SystemMetrics struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	DiskPercent   float64   `json:"disk_percent"`
	LoadAverage   []float64 `json:"load_average"`
}

// HealthMessage is sent by the agent to report health status
type HealthMessage struct {
	Type   string                 `json:"type"`
	Status string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Checks map[string]HealthCheck `json:"checks"`
	System *SystemMetrics         `json:"system,omitempty"`
}

// NewHealthMessage creates a new health message
func NewHealthMessage(status string, checks map[string]HealthCheck, system *SystemMetrics) *HealthMessage {
	return &HealthMessage{
		Type:   TypeHealth,
		Status: status,
		Checks: checks,
		System: system,
	}
}

// LogRequestMessage is sent by the server to request logs
type LogRequestMessage struct {
	Type    string   `json:"type"`
	ID      string   `json:"id"`
	Paths   []string `json:"paths"`
	Since   string   `json:"since,omitempty"`
	Until   string   `json:"until,omitempty"`
	Limit   int      `json:"limit"`
	Filter  string   `json:"filter,omitempty"`
	Timeout int      `json:"timeout"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Line      string `json:"line"`
	Timestamp string `json:"timestamp"`
}

// LogResponseMessage is sent by the agent with log entries
type LogResponseMessage struct {
	Type      string     `json:"type"`
	ID        string     `json:"id"`
	Entries   []LogEntry `json:"entries"`
	Truncated bool       `json:"truncated"`
}

// NewLogResponseMessage creates a new log response message
func NewLogResponseMessage(id string, entries []LogEntry, truncated bool) *LogResponseMessage {
	return &LogResponseMessage{
		Type:      TypeLogResponse,
		ID:        id,
		Entries:   entries,
		Truncated: truncated,
	}
}

// ParseMessage parses a JSON message and returns the type
func ParseMessage(data []byte) (string, error) {
	var base BaseMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return "", err
	}
	return base.Type, nil
}

// ParseCommandMessage parses a command message
func ParseCommandMessage(data []byte) (*CommandMessage, error) {
	var msg CommandMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ParseAuthResponseMessage parses an auth response message
func ParseAuthResponseMessage(data []byte) (*AuthResponseMessage, error) {
	var msg AuthResponseMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ParseLogRequestMessage parses a log request message
func ParseLogRequestMessage(data []byte) (*LogRequestMessage, error) {
	var msg LogRequestMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
