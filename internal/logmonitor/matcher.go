package logmonitor

import (
	"strings"
	"sync"
)

// Match represents a matched error with context
type Match struct {
	Source        string
	ErrorLine     string
	ContextBefore []string
	ContextAfter  []string
}

// MatchHandler is called when an error is matched with full context
type MatchHandler func(match Match)

// Matcher matches lines against error patterns and captures context
type Matcher struct {
	patterns     []string
	contextLines int
	handler      MatchHandler

	// Ring buffer for context before
	buffer      []string
	bufferPos   int
	bufferCount int

	// State for context after
	capturing         bool
	captureMatch      Match
	captureAfterCount int

	mu sync.Mutex
}

// NewMatcher creates a new pattern matcher
func NewMatcher(patterns []string, contextLines int, handler MatchHandler) *Matcher {
	if contextLines <= 0 {
		contextLines = 20
	}

	return &Matcher{
		patterns:     patterns,
		contextLines: contextLines,
		handler:      handler,
		buffer:       make([]string, contextLines),
		bufferPos:    0,
		bufferCount:  0,
	}
}

// ProcessLine processes a single line from a log file
func (m *Matcher) ProcessLine(source, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If we're capturing context after an error
	if m.capturing {
		m.captureMatch.ContextAfter = append(m.captureMatch.ContextAfter, line)
		m.captureAfterCount++

		if m.captureAfterCount >= m.contextLines {
			// Done capturing, emit the match
			m.emitMatch()
		}
	}

	// Check if this line matches any error pattern
	if m.matchesPattern(line) {
		// If we were capturing context for a previous match, emit it first
		if m.capturing {
			m.emitMatch()
		}

		// Start a new match
		m.captureMatch = Match{
			Source:        source,
			ErrorLine:     line,
			ContextBefore: m.getContextBefore(),
			ContextAfter:  make([]string, 0, m.contextLines),
		}
		m.capturing = true
		m.captureAfterCount = 0
	}

	// Add line to context buffer (ring buffer)
	m.buffer[m.bufferPos] = line
	m.bufferPos = (m.bufferPos + 1) % m.contextLines
	if m.bufferCount < m.contextLines {
		m.bufferCount++
	}
}

// Flush emits any pending match (call when done processing)
func (m *Matcher) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.capturing {
		m.emitMatch()
	}
}

// matchesPattern checks if a line matches any error pattern
func (m *Matcher) matchesPattern(line string) bool {
	lineLower := strings.ToLower(line)

	for _, pattern := range m.patterns {
		// Case-insensitive substring match
		if strings.Contains(lineLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// getContextBefore returns the context lines before the current position
func (m *Matcher) getContextBefore() []string {
	result := make([]string, 0, m.bufferCount)

	if m.bufferCount == 0 {
		return result
	}

	// Read from ring buffer in order
	if m.bufferCount < m.contextLines {
		// Buffer not full yet, start from 0
		for i := 0; i < m.bufferCount; i++ {
			result = append(result, m.buffer[i])
		}
	} else {
		// Buffer full, read from bufferPos to end, then start to bufferPos
		for i := 0; i < m.contextLines; i++ {
			idx := (m.bufferPos + i) % m.contextLines
			result = append(result, m.buffer[idx])
		}
	}

	return result
}

// emitMatch emits the current match and resets state
func (m *Matcher) emitMatch() {
	if m.handler != nil {
		m.handler(m.captureMatch)
	}
	m.capturing = false
	m.captureAfterCount = 0
}

// UpdatePatterns updates the error patterns
func (m *Matcher) UpdatePatterns(patterns []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patterns = patterns
}

// UpdateContextLines updates the context line count
func (m *Matcher) UpdateContextLines(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if count <= 0 {
		count = 20
	}

	// Resize buffer if needed
	if count != m.contextLines {
		m.buffer = make([]string, count)
		m.bufferPos = 0
		m.bufferCount = 0
		m.contextLines = count
	}
}
