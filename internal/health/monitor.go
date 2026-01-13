package health

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// SendFunc is a function that sends a message
type SendFunc func(msg interface{}) error

// Monitor runs periodic health reporting
type Monitor struct {
	send   SendFunc
	doneCh chan struct{}
	wg     sync.WaitGroup
}

// NewMonitor creates a new health monitor
func NewMonitor(send SendFunc) *Monitor {
	return &Monitor{
		send:   send,
		doneCh: make(chan struct{}),
	}
}

// Start begins periodic health reporting
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
		m.reportHealth()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.doneCh:
				return
			case <-ticker.C:
				m.reportHealth()
			}
		}
	}()
}

// Stop stops the health monitor
func (m *Monitor) Stop() {
	close(m.doneCh)
	m.wg.Wait()
}

// reportHealth collects and sends system metrics
func (m *Monitor) reportHealth() {
	var cpuPercent float64
	var memUsed, memTotal, diskUsed, diskTotal uint64
	var loadAvg float64

	// CPU percent (1 second sample)
	if cpuPct, err := cpu.Percent(time.Second, false); err == nil && len(cpuPct) > 0 {
		cpuPercent = cpuPct[0]
	}

	// Memory
	if memInfo, err := mem.VirtualMemory(); err == nil {
		memUsed = memInfo.Used
		memTotal = memInfo.Total
	}

	// Disk (root partition)
	if diskInfo, err := disk.Usage("/"); err == nil {
		diskUsed = diskInfo.Used
		diskTotal = diskInfo.Total
	}

	// Load average
	if loadInfo, err := load.Avg(); err == nil {
		loadAvg = loadInfo.Load1
	}

	msg := messages.NewHealthMessage(cpuPercent, memUsed, memTotal, diskUsed, diskTotal, loadAvg)
	if err := m.send(msg); err != nil {
		log.Printf("Failed to send health message: %v", err)
	}
}
