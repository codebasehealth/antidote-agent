package health

import (
	"context"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/config"
	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// SendFunc is a function that sends a message
type SendFunc func(msg interface{}) error

// Monitor runs periodic health checks
type Monitor struct {
	cfg    *config.Config
	send   SendFunc
	doneCh chan struct{}
	wg     sync.WaitGroup
}

// NewMonitor creates a new health monitor
func NewMonitor(cfg *config.Config, send SendFunc) *Monitor {
	return &Monitor{
		cfg:    cfg,
		send:   send,
		doneCh: make(chan struct{}),
	}
}

// Start begins periodic health checks
func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		interval = 60 * time.Second
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run immediately
		m.runHealthChecks()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.doneCh:
				return
			case <-ticker.C:
				m.runHealthChecks()
			}
		}
	}()
}

// Stop stops the health monitor
func (m *Monitor) Stop() {
	close(m.doneCh)
	m.wg.Wait()
}

// runHealthChecks performs all health checks and reports status
func (m *Monitor) runHealthChecks() {
	checks := make(map[string]messages.HealthCheck)
	overallStatus := "healthy"

	// Run configured health check actions
	for name, action := range m.cfg.Actions {
		// Only run actions that have an interval (health check actions)
		// For now, we'll check actions named "health_check" or similar
		if name == "health_check" || name == "healthcheck" {
			check := m.runHealthCheckAction(name, action)
			checks[name] = check
			if check.Status != "passing" {
				overallStatus = "degraded"
			}
		}
	}

	// Collect system metrics
	system := m.collectSystemMetrics()

	// Check for critical thresholds
	if system != nil {
		if system.CPUPercent > 90 || system.MemoryPercent > 90 || system.DiskPercent > 90 {
			if overallStatus == "healthy" {
				overallStatus = "degraded"
			}
		}
	}

	// Send health message
	msg := messages.NewHealthMessage(overallStatus, checks, system)
	if err := m.send(msg); err != nil {
		log.Printf("Failed to send health message: %v", err)
	}
}

// runHealthCheckAction runs a health check command and returns the result
func (m *Monitor) runHealthCheckAction(name string, action config.Action) messages.HealthCheck {
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), action.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", action.Command)
	if action.WorkingDir != "" {
		cmd.Dir = action.WorkingDir
	}

	err := cmd.Run()

	latencyMs := int(time.Since(startTime).Milliseconds())
	status := "passing"
	if err != nil {
		status = "failing"
	}

	return messages.HealthCheck{
		Status:    status,
		LatencyMs: latencyMs,
		LastCheck: time.Now().UTC().Format(time.RFC3339),
	}
}

// collectSystemMetrics gathers system resource metrics
func (m *Monitor) collectSystemMetrics() *messages.SystemMetrics {
	metrics := &messages.SystemMetrics{}

	// CPU percent (1 second sample)
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		metrics.CPUPercent = cpuPercent[0]
	}

	// Memory percent
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		metrics.MemoryPercent = memInfo.UsedPercent
	}

	// Disk percent (root partition)
	diskInfo, err := disk.Usage("/")
	if err == nil {
		metrics.DiskPercent = diskInfo.UsedPercent
	}

	// Load average
	loadInfo, err := load.Avg()
	if err == nil {
		metrics.LoadAverage = []float64{loadInfo.Load1, loadInfo.Load5, loadInfo.Load15}
	}

	return metrics
}
