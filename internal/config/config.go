package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the agent configuration
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Connection ConnectionConfig `yaml:"connection"`
	Actions    map[string]Action `yaml:"actions"`
	Logs       LogsConfig       `yaml:"logs"`
}

// ServerConfig contains server identification
type ServerConfig struct {
	Name        string            `yaml:"name"`
	Environment string            `yaml:"environment"`
	Tags        map[string]string `yaml:"tags"`
}

// ConnectionConfig contains WebSocket connection settings
type ConnectionConfig struct {
	Endpoint  string          `yaml:"endpoint"`
	Token     string          `yaml:"token"`
	Heartbeat time.Duration   `yaml:"heartbeat"`
	Reconnect ReconnectConfig `yaml:"reconnect"`
}

// ReconnectConfig contains reconnection settings
type ReconnectConfig struct {
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
	Multiplier   float64       `yaml:"multiplier"`
}

// Action represents a pre-defined action
type Action struct {
	Description      string        `yaml:"description"`
	Command          string        `yaml:"command"`
	Timeout          time.Duration `yaml:"timeout"`
	WorkingDir       string        `yaml:"working_dir"`
	Env              map[string]string `yaml:"env"`
	User             string        `yaml:"user"`
	RequiresApproval bool          `yaml:"requires_approval"`
}

// LogsConfig contains log streaming configuration
type LogsConfig struct {
	Paths []LogPath `yaml:"paths"`
}

// LogPath represents a log file path configuration
type LogPath struct {
	Path string `yaml:"path"`
	Type string `yaml:"type"`
}

// Load loads configuration from a file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Substitute environment variables
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	// Validate
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// expandEnvVars expands ${VAR} and ${VAR:-default} patterns in the string
func expandEnvVars(s string) string {
	// Pattern for ${VAR} or ${VAR:-default}
	re := regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

	return re.ReplaceAllStringFunc(s, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultVal := ""
		if len(parts) >= 3 {
			defaultVal = parts[2]
		}

		if val := os.Getenv(varName); val != "" {
			return val
		}
		return defaultVal
	})
}

// applyDefaults sets default values for missing configuration
func applyDefaults(cfg *Config) {
	if cfg.Server.Environment == "" {
		cfg.Server.Environment = "production"
	}

	if cfg.Connection.Heartbeat == 0 {
		cfg.Connection.Heartbeat = 30 * time.Second
	}

	if cfg.Connection.Reconnect.InitialDelay == 0 {
		cfg.Connection.Reconnect.InitialDelay = 1 * time.Second
	}

	if cfg.Connection.Reconnect.MaxDelay == 0 {
		cfg.Connection.Reconnect.MaxDelay = 30 * time.Second
	}

	if cfg.Connection.Reconnect.Multiplier == 0 {
		cfg.Connection.Reconnect.Multiplier = 2.0
	}

	// Apply defaults to actions
	for name, action := range cfg.Actions {
		if action.Timeout == 0 {
			action.Timeout = 60 * time.Second
		}
		cfg.Actions[name] = action
	}
}

// validate checks the configuration for required fields
func validate(cfg *Config) error {
	if cfg.Connection.Endpoint == "" {
		return fmt.Errorf("connection.endpoint is required")
	}

	if cfg.Connection.Token == "" {
		return fmt.Errorf("connection.token is required")
	}

	if !strings.HasPrefix(cfg.Connection.Endpoint, "ws://") && !strings.HasPrefix(cfg.Connection.Endpoint, "wss://") {
		return fmt.Errorf("connection.endpoint must start with ws:// or wss://")
	}

	return nil
}

// FindConfigFile searches for a config file in standard locations
func FindConfigFile() (string, error) {
	paths := []string{
		"./antidote.yml",
		"./antidote.yaml",
		"/etc/antidote/antidote.yml",
		"/etc/antidote/antidote.yaml",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("no config file found in standard locations")
}

// GetActionNames returns a list of available action names
func (c *Config) GetActionNames() []string {
	names := make([]string, 0, len(c.Actions))
	for name := range c.Actions {
		names = append(names, name)
	}
	return names
}

// GetAction returns an action by name
func (c *Config) GetAction(name string) (Action, bool) {
	action, ok := c.Actions[name]
	return action, ok
}
