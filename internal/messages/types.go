package messages

import (
	"encoding/json"
	"time"
)

// Message types for agent-cloud protocol
const (
	TypeAuth      = "auth"
	TypeAuthOK    = "auth_ok"
	TypeAuthError = "auth_error"
	TypeDiscover  = "discover"
	TypeDiscovery = "discovery"
	TypeCommand   = "command"
	TypeOutput    = "output"
	TypeComplete  = "complete"
	TypeRejected  = "rejected"
	TypeHealth    = "health"
	TypeHeartbeat = "heartbeat"
)

// BaseMessage contains common fields
type BaseMessage struct {
	Type string `json:"type"`
}

// AuthMessage - agent authenticates with cloud
type AuthMessage struct {
	Type         string `json:"type"`
	Token        string `json:"token"`
	AgentVersion string `json:"agent_version"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
}

func NewAuthMessage(token, version, hostname, os, arch string) *AuthMessage {
	return &AuthMessage{
		Type:         TypeAuth,
		Token:        token,
		AgentVersion: version,
		Hostname:     hostname,
		OS:           os,
		Arch:         arch,
	}
}

// AuthOKMessage - cloud confirms authentication
type AuthOKMessage struct {
	Type     string `json:"type"`
	ServerID string `json:"server_id"`
}

// AuthErrorMessage - cloud rejects authentication
type AuthErrorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// DiscoverRequest - cloud asks agent to discover server state
type DiscoverRequest struct {
	Type string `json:"type"`
}

// DiscoveryMessage - agent reports what's on the server
type DiscoveryMessage struct {
	Type       string            `json:"type"`
	Hostname   string            `json:"hostname"`
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	Distro     string            `json:"distro,omitempty"`
	Kernel     string            `json:"kernel,omitempty"`
	Uptime     int64             `json:"uptime"`
	Services   []ServiceInfo     `json:"services"`
	Languages  []LanguageInfo    `json:"languages"`
	Apps       []AppInfo         `json:"apps"`
	Docker     *DockerInfo       `json:"docker,omitempty"`
	System     SystemInfo        `json:"system"`
}

func NewDiscoveryMessage() *DiscoveryMessage {
	return &DiscoveryMessage{
		Type: TypeDiscovery,
	}
}

type ServiceInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // running, stopped, not_found
	Version string `json:"version,omitempty"`
}

type LanguageInfo struct {
	Name    string `json:"name"` // php, node, python, ruby, go
	Version string `json:"version"`
	Path    string `json:"path"`
}

type AppInfo struct {
	Path      string     `json:"path"`
	Framework string     `json:"framework,omitempty"` // laravel, rails, django, nextjs
	GitRemote string     `json:"git_remote,omitempty"`
	GitBranch string     `json:"git_branch,omitempty"`
	GitCommit string     `json:"git_commit,omitempty"`
	Config    *AppConfig `json:"config,omitempty"` // parsed from antidote.yml
}

// AppConfig represents the parsed antidote.yml configuration
type AppConfig struct {
	Version          int                       `json:"version" yaml:"version"`
	App              AppConfigApp              `json:"app" yaml:"app"`
	TrustLevel       string                    `json:"trust_level" yaml:"trust_level"`
	Actions          map[string]AppConfigAction `json:"actions" yaml:"actions"`
	ApprovalRequired []AppConfigApproval       `json:"approval_required" yaml:"approval_required"`
	Deny             []string                  `json:"deny" yaml:"deny"`
	Logs             []string                  `json:"logs" yaml:"logs"`
	Health           *AppConfigHealth          `json:"health,omitempty" yaml:"health"`
}

type AppConfigApp struct {
	Name      string `json:"name" yaml:"name"`
	Framework string `json:"framework" yaml:"framework"`
}

type AppConfigAction struct {
	Command string `json:"command" yaml:"command"`
	Label   string `json:"label" yaml:"label"`
	Icon    string `json:"icon,omitempty" yaml:"icon"`
	Confirm bool   `json:"confirm,omitempty" yaml:"confirm"`
}

type AppConfigApproval struct {
	Pattern string `json:"pattern" yaml:"pattern"`
	Reason  string `json:"reason" yaml:"reason"`
}

type AppConfigHealth struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Interval string `json:"interval" yaml:"interval"`
}

type DockerInfo struct {
	Version    string          `json:"version"`
	Containers []ContainerInfo `json:"containers"`
}

type ContainerInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type SystemInfo struct {
	CPUCores    int     `json:"cpu_cores"`
	MemoryTotal uint64  `json:"memory_total"`
	MemoryFree  uint64  `json:"memory_free"`
	DiskTotal   uint64  `json:"disk_total"`
	DiskFree    uint64  `json:"disk_free"`
	LoadAvg     float64 `json:"load_avg"`
}

// CommandMessage - cloud tells agent to run a command
type CommandMessage struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Command    string            `json:"command"`
	WorkingDir string            `json:"working_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Timeout    int               `json:"timeout,omitempty"` // seconds, 0 = default
}

func ParseCommandMessage(data []byte) (*CommandMessage, error) {
	var msg CommandMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// OutputMessage - agent streams command output
type OutputMessage struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Stream    string `json:"stream"` // stdout or stderr
	Data      string `json:"data"`
	Timestamp string `json:"timestamp"`
}

func NewOutputMessage(id, stream, data string) *OutputMessage {
	return &OutputMessage{
		Type:      TypeOutput,
		ID:        id,
		Stream:    stream,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// CompleteMessage - agent reports command completion
type CompleteMessage struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Timestamp  string `json:"timestamp"`
}

func NewCompleteMessage(id string, exitCode int, durationMs int64) *CompleteMessage {
	return &CompleteMessage{
		Type:       TypeComplete,
		ID:         id,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// RejectedMessage - agent rejects a command for security reasons
type RejectedMessage struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Code      string `json:"code"`    // Error code (e.g., COMMAND_DENIED, PATH_TRAVERSAL)
	Message   string `json:"message"` // Human-readable error message
	Timestamp string `json:"timestamp"`
}

func NewRejectedMessage(id, code, message string) *RejectedMessage {
	return &RejectedMessage{
		Type:      TypeRejected,
		ID:        id,
		Code:      code,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// HealthMessage - agent reports system health
type HealthMessage struct {
	Type        string  `json:"type"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	LoadAvg     float64 `json:"load_avg"`
	Timestamp   string  `json:"timestamp"`
}

func NewHealthMessage(cpu float64, memUsed, memTotal, diskUsed, diskTotal uint64, load float64) *HealthMessage {
	return &HealthMessage{
		Type:        TypeHealth,
		CPUPercent:  cpu,
		MemoryUsed:  memUsed,
		MemoryTotal: memTotal,
		DiskUsed:    diskUsed,
		DiskTotal:   diskTotal,
		LoadAvg:     load,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// HeartbeatMessage - keep connection alive
type HeartbeatMessage struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

func NewHeartbeatMessage() *HeartbeatMessage {
	return &HeartbeatMessage{
		Type:      TypeHeartbeat,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// ParseMessage extracts the message type
func ParseMessage(data []byte) (string, error) {
	var base BaseMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return "", err
	}
	return base.Type, nil
}
