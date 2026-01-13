package logmonitor

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Default deduplication settings
const (
	DefaultRateWindow   = 5 * time.Minute  // Time window for rate limiting
	DefaultMaxPerWindow = 5                 // Max events per signature per window
	DefaultCleanupInterval = 10 * time.Minute
)

// DedupEntry tracks a single error signature
type DedupEntry struct {
	SignatureHash   string
	FirstSeen       time.Time
	LastSeen        time.Time
	OccurrenceCount int
	WindowStart     time.Time
	WindowCount     int
}

// Deduplicator prevents duplicate error events from flooding the system
type Deduplicator struct {
	entries     map[string]*DedupEntry
	rateWindow  time.Duration
	maxPerWindow int

	mu       sync.Mutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewDeduplicator creates a new deduplicator
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{
		entries:      make(map[string]*DedupEntry),
		rateWindow:   DefaultRateWindow,
		maxPerWindow: DefaultMaxPerWindow,
		stopCh:       make(chan struct{}),
	}
}

// Start starts the background cleanup goroutine
func (d *Deduplicator) Start() {
	d.wg.Add(1)
	go d.cleanupLoop()
}

// Stop stops the deduplicator
func (d *Deduplicator) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

// ShouldEmit checks if an error should be emitted (returns true) or suppressed
// It also updates internal state and returns the dedup info
func (d *Deduplicator) ShouldEmit(errorLine string) (emit bool, entry *DedupEntry) {
	hash := d.computeSignature(errorLine)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	existing, found := d.entries[hash]
	if !found {
		// New error signature
		entry = &DedupEntry{
			SignatureHash:   hash,
			FirstSeen:       now,
			LastSeen:        now,
			OccurrenceCount: 1,
			WindowStart:     now,
			WindowCount:     1,
		}
		d.entries[hash] = entry
		return true, entry
	}

	// Update existing entry
	existing.LastSeen = now
	existing.OccurrenceCount++

	// Check rate limiting window
	if now.Sub(existing.WindowStart) > d.rateWindow {
		// Window expired, reset
		existing.WindowStart = now
		existing.WindowCount = 1
		return true, existing
	}

	// Within window - check count
	existing.WindowCount++
	if existing.WindowCount <= d.maxPerWindow {
		return true, existing
	}

	// Rate limited
	return false, existing
}

// GetEntry returns the dedup entry for an error (without modifying state)
func (d *Deduplicator) GetEntry(errorLine string) *DedupEntry {
	hash := d.computeSignature(errorLine)

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.entries[hash]
}

// computeSignature generates a hash for error deduplication
// Normalizes timestamps, IDs, and other variable parts
func (d *Deduplicator) computeSignature(errorLine string) string {
	normalized := d.normalizeError(errorLine)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes (16 hex chars)
}

// normalizeError normalizes an error line for signature generation
// Removes timestamps, request IDs, memory addresses, etc.
func (d *Deduplicator) normalizeError(errorLine string) string {
	// Remove common variable patterns
	// Order matters! More specific patterns (like UUIDs) must come before
	// more generic patterns (like Unix timestamps) to avoid partial matches
	patterns := []*regexp.Regexp{
		// UUIDs (must be before Unix timestamps to avoid partial matching)
		regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`),

		// ISO timestamps: 2026-01-13T17:52:46Z, 2026-01-13 17:52:46
		regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`),

		// Laravel/PSR-3 timestamp: [2026-01-13 17:52:46]
		regexp.MustCompile(`\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]`),

		// Memory addresses: 0x7fff5fbff8c0 (before Unix timestamps)
		regexp.MustCompile(`0x[0-9a-fA-F]+`),

		// Unix timestamps (10-13 digit numbers)
		regexp.MustCompile(`\b\d{10,13}\b`),

		// Request IDs (various formats)
		regexp.MustCompile(`request[_-]?id[=:]\s*[^\s,}\]]+`),

		// PIDs: pid=12345, process 12345
		regexp.MustCompile(`(pid[=:]\s*|process\s+)\d+`),

		// Port numbers in URLs (might vary)
		regexp.MustCompile(`:\d{4,5}/`),

		// Session IDs
		regexp.MustCompile(`session[_-]?id[=:]\s*[^\s,}\]]+`),
	}

	result := errorLine
	for _, pattern := range patterns {
		result = pattern.ReplaceAllString(result, "")
	}

	// Normalize whitespace
	result = strings.Join(strings.Fields(result), " ")

	return result
}

// cleanupLoop periodically removes old entries
func (d *Deduplicator) cleanupLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(DefaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.cleanup()
		}
	}
}

// cleanup removes entries that haven't been seen recently
func (d *Deduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-d.rateWindow * 2)

	for hash, entry := range d.entries {
		if entry.LastSeen.Before(cutoff) {
			delete(d.entries, hash)
		}
	}
}

// SetRateWindow sets the rate limiting window
func (d *Deduplicator) SetRateWindow(window time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rateWindow = window
}

// SetMaxPerWindow sets the max events per window
func (d *Deduplicator) SetMaxPerWindow(max int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxPerWindow = max
}

// Stats returns deduplication statistics
func (d *Deduplicator) Stats() (uniqueErrors int, totalOccurrences int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	uniqueErrors = len(d.entries)
	for _, entry := range d.entries {
		totalOccurrences += entry.OccurrenceCount
	}
	return
}
