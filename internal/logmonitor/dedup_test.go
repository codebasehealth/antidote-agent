package logmonitor

import (
	"testing"
	"time"
)

func TestDeduplicatorNewError(t *testing.T) {
	dedup := NewDeduplicator()

	emit, entry := dedup.ShouldEmit("ERROR: something went wrong")

	if !emit {
		t.Error("expected first occurrence to emit")
	}
	if entry.OccurrenceCount != 1 {
		t.Errorf("expected occurrence count 1, got %d", entry.OccurrenceCount)
	}
	if entry.SignatureHash == "" {
		t.Error("expected signature hash to be set")
	}
}

func TestDeduplicatorDuplicateError(t *testing.T) {
	dedup := NewDeduplicator()

	errorLine := "ERROR: database connection failed"

	emit1, entry1 := dedup.ShouldEmit(errorLine)
	emit2, entry2 := dedup.ShouldEmit(errorLine)

	if !emit1 {
		t.Error("expected first occurrence to emit")
	}
	if !emit2 {
		t.Error("expected second occurrence to emit (within rate limit)")
	}
	if entry2.OccurrenceCount != 2 {
		t.Errorf("expected occurrence count 2, got %d", entry2.OccurrenceCount)
	}
	if entry1.SignatureHash != entry2.SignatureHash {
		t.Error("expected same signature hash for same error")
	}
}

func TestDeduplicatorRateLimit(t *testing.T) {
	dedup := NewDeduplicator()
	dedup.SetMaxPerWindow(3)

	errorLine := "ERROR: rate limited error"

	// First 3 should emit
	for i := 0; i < 3; i++ {
		emit, _ := dedup.ShouldEmit(errorLine)
		if !emit {
			t.Errorf("expected occurrence %d to emit", i+1)
		}
	}

	// 4th and beyond should be suppressed
	emit, entry := dedup.ShouldEmit(errorLine)
	if emit {
		t.Error("expected occurrence 4 to be suppressed")
	}
	if entry.OccurrenceCount != 4 {
		t.Errorf("expected occurrence count 4, got %d", entry.OccurrenceCount)
	}
}

func TestDeduplicatorDifferentErrors(t *testing.T) {
	dedup := NewDeduplicator()

	emit1, entry1 := dedup.ShouldEmit("ERROR: error type A")
	emit2, entry2 := dedup.ShouldEmit("ERROR: error type B")

	if !emit1 || !emit2 {
		t.Error("expected both different errors to emit")
	}
	if entry1.SignatureHash == entry2.SignatureHash {
		t.Error("expected different signature hashes for different errors")
	}
}

func TestDeduplicatorNormalizesTimestamps(t *testing.T) {
	dedup := NewDeduplicator()

	// Same error with different timestamps should have same signature
	error1 := "[2026-01-13 10:00:00] ERROR: connection failed"
	error2 := "[2026-01-13 10:05:00] ERROR: connection failed"

	_, entry1 := dedup.ShouldEmit(error1)
	_, entry2 := dedup.ShouldEmit(error2)

	if entry1.SignatureHash != entry2.SignatureHash {
		t.Errorf("expected same signature for errors differing only in timestamp: %s vs %s",
			entry1.SignatureHash, entry2.SignatureHash)
	}
}

func TestDeduplicatorNormalizesUUIDs(t *testing.T) {
	dedup := NewDeduplicator()

	// Same error with different UUIDs should have same signature
	error1 := "ERROR: Request 550e8400-e29b-41d4-a716-446655440000 failed"
	error2 := "ERROR: Request 6ba7b810-9dad-11d1-80b4-00c04fd430c8 failed"

	_, entry1 := dedup.ShouldEmit(error1)
	_, entry2 := dedup.ShouldEmit(error2)

	if entry1.SignatureHash != entry2.SignatureHash {
		t.Errorf("expected same signature for errors differing only in UUID: %s vs %s",
			entry1.SignatureHash, entry2.SignatureHash)
	}
}

func TestDeduplicatorStats(t *testing.T) {
	dedup := NewDeduplicator()

	dedup.ShouldEmit("ERROR: error A")
	dedup.ShouldEmit("ERROR: error A")
	dedup.ShouldEmit("ERROR: error B")
	dedup.ShouldEmit("ERROR: error A")
	dedup.ShouldEmit("ERROR: error C")

	unique, total := dedup.Stats()

	if unique != 3 {
		t.Errorf("expected 3 unique errors, got %d", unique)
	}
	if total != 5 {
		t.Errorf("expected 5 total occurrences, got %d", total)
	}
}

func TestDeduplicatorWindowReset(t *testing.T) {
	dedup := NewDeduplicator()
	dedup.SetRateWindow(50 * time.Millisecond)
	dedup.SetMaxPerWindow(2)

	errorLine := "ERROR: window reset test"

	// First 2 should emit
	dedup.ShouldEmit(errorLine)
	dedup.ShouldEmit(errorLine)

	// 3rd should be suppressed
	emit, _ := dedup.ShouldEmit(errorLine)
	if emit {
		t.Error("expected 3rd occurrence to be suppressed")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should emit again after window expires
	emit, entry := dedup.ShouldEmit(errorLine)
	if !emit {
		t.Error("expected occurrence after window reset to emit")
	}
	if entry.OccurrenceCount != 4 {
		t.Errorf("expected occurrence count 4, got %d", entry.OccurrenceCount)
	}
}
