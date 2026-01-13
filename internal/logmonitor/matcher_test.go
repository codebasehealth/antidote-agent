package logmonitor

import (
	"testing"
)

func TestMatcherBasicMatch(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"ERROR", "Exception"}, 3, func(m Match) {
		matches = append(matches, m)
	})

	// Add some context lines
	matcher.ProcessLine("test.log", "line 1 - normal")
	matcher.ProcessLine("test.log", "line 2 - normal")
	matcher.ProcessLine("test.log", "line 3 - normal")

	// Error line
	matcher.ProcessLine("test.log", "line 4 - ERROR: something went wrong")

	// Context after
	matcher.ProcessLine("test.log", "line 5 - normal")
	matcher.ProcessLine("test.log", "line 6 - normal")
	matcher.ProcessLine("test.log", "line 7 - normal")

	// Flush to emit pending match
	matcher.Flush()

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]
	if m.Source != "test.log" {
		t.Errorf("expected source 'test.log', got '%s'", m.Source)
	}
	if m.ErrorLine != "line 4 - ERROR: something went wrong" {
		t.Errorf("unexpected error line: %s", m.ErrorLine)
	}
	if len(m.ContextBefore) != 3 {
		t.Errorf("expected 3 context before lines, got %d", len(m.ContextBefore))
	}
	if len(m.ContextAfter) != 3 {
		t.Errorf("expected 3 context after lines, got %d", len(m.ContextAfter))
	}
}

func TestMatcherCaseInsensitive(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"error"}, 2, func(m Match) {
		matches = append(matches, m)
	})

	matcher.ProcessLine("test.log", "This is an ERROR message")
	matcher.ProcessLine("test.log", "normal line")
	matcher.ProcessLine("test.log", "normal line")
	matcher.Flush()

	if len(matches) != 1 {
		t.Fatalf("expected 1 match (case insensitive), got %d", len(matches))
	}
}

func TestMatcherMultipleMatches(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"ERROR"}, 1, func(m Match) {
		matches = append(matches, m)
	})

	matcher.ProcessLine("test.log", "normal")
	matcher.ProcessLine("test.log", "ERROR 1")
	matcher.ProcessLine("test.log", "normal")
	matcher.ProcessLine("test.log", "ERROR 2")
	matcher.ProcessLine("test.log", "normal")
	matcher.Flush()

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestMatcherNoMatch(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"CRITICAL"}, 2, func(m Match) {
		matches = append(matches, m)
	})

	matcher.ProcessLine("test.log", "normal line 1")
	matcher.ProcessLine("test.log", "normal line 2")
	matcher.ProcessLine("test.log", "info: everything ok")
	matcher.Flush()

	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestMatcherMultiplePatterns(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"ERROR", "FATAL", "Exception"}, 1, func(m Match) {
		matches = append(matches, m)
	})

	matcher.ProcessLine("test.log", "normal")
	matcher.ProcessLine("test.log", "FATAL: crash")
	matcher.ProcessLine("test.log", "normal")
	matcher.ProcessLine("test.log", "NullPointerException occurred")
	matcher.ProcessLine("test.log", "normal")
	matcher.Flush()

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches from different patterns, got %d", len(matches))
	}
}

func TestMatcherContextBuffer(t *testing.T) {
	var matches []Match
	matcher := NewMatcher([]string{"ERROR"}, 5, func(m Match) {
		matches = append(matches, m)
	})

	// Add more lines than buffer size
	for i := 0; i < 10; i++ {
		matcher.ProcessLine("test.log", "context line")
	}
	matcher.ProcessLine("test.log", "ERROR: problem")
	matcher.ProcessLine("test.log", "after 1")
	matcher.ProcessLine("test.log", "after 2")
	matcher.ProcessLine("test.log", "after 3")
	matcher.ProcessLine("test.log", "after 4")
	matcher.ProcessLine("test.log", "after 5")
	matcher.Flush()

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	// Should have exactly 5 context before lines (buffer size)
	if len(matches[0].ContextBefore) != 5 {
		t.Errorf("expected 5 context before lines, got %d", len(matches[0].ContextBefore))
	}
}
