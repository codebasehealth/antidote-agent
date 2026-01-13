package logmonitor

import (
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/messages"
)

// SendFunc is a function that sends a message to the cloud
type SendFunc func(msg interface{}) error

// AppDiscovery provides app discovery info for matching configs to paths
type AppDiscovery interface {
	GetApps() []messages.AppInfo
}

// Monitor orchestrates log monitoring for all configured apps
type Monitor struct {
	send        SendFunc
	discovery   AppDiscovery
	configStore *ConfigStore
	dedup       *Deduplicator

	// Per-app monitors
	appMonitors map[string]*AppMonitor // keyed by app path

	mu     sync.Mutex
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// AppMonitor monitors logs for a single application
type AppMonitor struct {
	config   *Config
	tailers  []*Tailer
	matchers []*Matcher
}

// NewMonitor creates a new log monitor
func NewMonitor(send SendFunc, discovery AppDiscovery) *Monitor {
	return &Monitor{
		send:        send,
		discovery:   discovery,
		configStore: NewConfigStore(),
		dedup:       NewDeduplicator(),
		appMonitors: make(map[string]*AppMonitor),
		stopCh:      make(chan struct{}),
	}
}

// Start starts the monitor
func (m *Monitor) Start() {
	m.dedup.Start()
}

// Stop stops all monitoring
func (m *Monitor) Stop() {
	close(m.stopCh)

	m.mu.Lock()
	for _, appMon := range m.appMonitors {
		for _, tailer := range appMon.tailers {
			tailer.Stop()
		}
	}
	m.appMonitors = make(map[string]*AppMonitor)
	m.mu.Unlock()

	m.dedup.Stop()
	m.wg.Wait()
}

// UpdateConfig updates the monitoring configuration from the cloud
func (m *Monitor) UpdateConfig(msg *messages.MonitoringConfigMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("Received monitoring config with %d apps", len(msg.Apps))

	// Update config store
	m.configStore.UpdateFromMessage(msg)

	// Match configs to discovered apps
	m.matchConfigsToApps()

	// Restart monitoring with new config
	m.restartMonitoring()
}

// matchConfigsToApps matches repo configs to discovered app paths
func (m *Monitor) matchConfigsToApps() {
	if m.discovery == nil {
		log.Printf("No discovery provider - cannot match configs to apps")
		return
	}

	apps := m.discovery.GetApps()
	log.Printf("Matching configs to %d discovered apps", len(apps))

	for _, app := range apps {
		if app.GitRemote == "" {
			continue
		}

		// Extract repo full name from git remote
		repoFullName := extractRepoFullName(app.GitRemote)
		if repoFullName == "" {
			continue
		}

		// Find config for this repo
		config := m.configStore.GetByRepoFullName(repoFullName)
		if config != nil {
			config.AppPath = app.Path
			log.Printf("Matched repo %s to path %s", repoFullName, app.Path)
		}
	}
}

// restartMonitoring stops current monitors and starts new ones based on config
func (m *Monitor) restartMonitoring() {
	// Stop existing monitors
	for _, appMon := range m.appMonitors {
		for _, tailer := range appMon.tailers {
			tailer.Stop()
		}
	}
	m.appMonitors = make(map[string]*AppMonitor)

	// Start monitors for configured apps
	for _, config := range m.configStore.GetConfigured() {
		m.startAppMonitor(config)
	}
}

// startAppMonitor starts monitoring for a single app
func (m *Monitor) startAppMonitor(config *Config) {
	appMon := &AppMonitor{
		config:   config,
		tailers:  make([]*Tailer, 0),
		matchers: make([]*Matcher, 0),
	}

	log.Printf("Starting log monitor for %s at %s", config.RepoFullName, config.AppPath)

	// Create a matcher for this app
	matcher := NewMatcher(config.ErrorPatterns, config.ContextLines, func(match Match) {
		m.handleMatch(config, match)
	})
	appMon.matchers = append(appMon.matchers, matcher)

	// Create tailers for each log path
	for _, logPath := range config.LogPaths {
		fullPath := filepath.Join(config.AppPath, logPath)

		// Handle glob patterns
		matches, err := filepath.Glob(fullPath)
		if err != nil || len(matches) == 0 {
			// Not a glob or no matches - try the path directly
			matches = []string{fullPath}
		}

		for _, path := range matches {
			tailer := NewTailer(path, func(source, line string) {
				matcher.ProcessLine(source, line)
			})

			if err := tailer.Start(); err != nil {
				log.Printf("Failed to start tailer for %s: %v", path, err)
				continue
			}

			appMon.tailers = append(appMon.tailers, tailer)
			log.Printf("  Tailing: %s", path)
		}
	}

	m.appMonitors[config.AppPath] = appMon
}

// handleMatch handles a matched error
func (m *Monitor) handleMatch(config *Config, match Match) {
	// Check deduplication
	shouldEmit, entry := m.dedup.ShouldEmit(match.ErrorLine)
	if !shouldEmit {
		log.Printf("Suppressed duplicate error (count: %d): %s",
			entry.OccurrenceCount, truncate(match.ErrorLine, 80))
		return
	}

	// Create error event message
	msg := messages.NewErrorEventMessage(
		config.AppPath,
		config.RepoFullName,
		match.Source,
		match.ErrorLine,
		match.ContextBefore,
		match.ContextAfter,
		entry.OccurrenceCount,
		entry.FirstSeen.UTC().Format(time.RFC3339),
		entry.SignatureHash,
	)

	// Send to cloud
	if err := m.send(msg); err != nil {
		log.Printf("Failed to send error event: %v", err)
		return
	}

	log.Printf("Sent error event: %s (count: %d)", truncate(match.ErrorLine, 60), entry.OccurrenceCount)
}

// extractRepoFullName extracts "owner/repo" from a git remote URL
func extractRepoFullName(gitRemote string) string {
	// Handle SSH format: git@github.com:owner/repo.git
	if len(gitRemote) > 4 && gitRemote[:4] == "git@" {
		// Find the colon
		colonIdx := -1
		for i, c := range gitRemote {
			if c == ':' {
				colonIdx = i
				break
			}
		}
		if colonIdx > 0 {
			path := gitRemote[colonIdx+1:]
			// Remove .git suffix
			if len(path) > 4 && path[len(path)-4:] == ".git" {
				path = path[:len(path)-4]
			}
			return path
		}
	}

	// Handle HTTPS format: https://github.com/owner/repo.git
	// Find the last two path segments
	lastSlash := -1
	secondLastSlash := -1
	for i := len(gitRemote) - 1; i >= 0; i-- {
		if gitRemote[i] == '/' {
			if lastSlash == -1 {
				lastSlash = i
			} else {
				secondLastSlash = i
				break
			}
		}
	}

	if secondLastSlash > 0 && lastSlash > secondLastSlash {
		path := gitRemote[secondLastSlash+1:]
		// Remove .git suffix
		if len(path) > 4 && path[len(path)-4:] == ".git" {
			path = path[:len(path)-4]
		}
		return path
	}

	return ""
}

// truncate truncates a string to maxLen with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
