package logmonitor

import (
	"github.com/codebasehealth/antidote-agent/internal/messages"
)

// Config holds the log monitoring configuration for a single app
type Config struct {
	// RepoFullName is the GitHub repo identifier (e.g., "owner/repo")
	RepoFullName string

	// AppPath is the absolute path to the application root on the server
	AppPath string

	// Framework is the detected framework (e.g., "laravel", "rails")
	Framework string

	// LogPaths are relative paths to log files from AppPath
	LogPaths []string

	// ErrorPatterns are strings to match for error detection
	ErrorPatterns []string

	// ContextLines is the number of lines to capture before/after an error
	ContextLines int
}

// NewConfigFromMessage creates a Config from a MonitoringAppConfig
// Note: AppPath must be set separately after discovery matching
func NewConfigFromMessage(msg messages.MonitoringAppConfig) *Config {
	contextLines := msg.ContextLines
	if contextLines <= 0 {
		contextLines = 20
	}

	return &Config{
		RepoFullName:  msg.RepoFullName,
		Framework:     msg.Framework,
		LogPaths:      msg.LogPaths,
		ErrorPatterns: msg.ErrorPatterns,
		ContextLines:  contextLines,
	}
}

// ConfigStore stores monitoring configurations and maps them to discovered apps
type ConfigStore struct {
	// configs maps repo_full_name to config
	configs map[string]*Config
}

// NewConfigStore creates a new ConfigStore
func NewConfigStore() *ConfigStore {
	return &ConfigStore{
		configs: make(map[string]*Config),
	}
}

// UpdateFromMessage updates the store with configs from a MonitoringConfigMessage
func (s *ConfigStore) UpdateFromMessage(msg *messages.MonitoringConfigMessage) {
	// Clear existing configs
	s.configs = make(map[string]*Config)

	for _, appConfig := range msg.Apps {
		s.configs[appConfig.RepoFullName] = NewConfigFromMessage(appConfig)
	}
}

// SetAppPath sets the app path for a config by repo full name
func (s *ConfigStore) SetAppPath(repoFullName, appPath string) {
	if cfg, ok := s.configs[repoFullName]; ok {
		cfg.AppPath = appPath
	}
}

// GetByRepoFullName returns the config for a repo
func (s *ConfigStore) GetByRepoFullName(repoFullName string) *Config {
	return s.configs[repoFullName]
}

// GetAll returns all configs
func (s *ConfigStore) GetAll() []*Config {
	result := make([]*Config, 0, len(s.configs))
	for _, cfg := range s.configs {
		result = append(result, cfg)
	}
	return result
}

// GetConfigured returns configs that have AppPath set (matched to discovered apps)
func (s *ConfigStore) GetConfigured() []*Config {
	result := make([]*Config, 0, len(s.configs))
	for _, cfg := range s.configs {
		if cfg.AppPath != "" {
			result = append(result, cfg)
		}
	}
	return result
}
