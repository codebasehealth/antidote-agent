package updater

import (
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		latest   string
		current  string
		expected bool
	}{
		{
			name:     "newer major version",
			latest:   "v1.0.0",
			current:  "v0.3.0",
			expected: true,
		},
		{
			name:     "newer minor version",
			latest:   "v0.4.0",
			current:  "v0.3.0",
			expected: true,
		},
		{
			name:     "newer patch version",
			latest:   "v0.3.1",
			current:  "v0.3.0",
			expected: true,
		},
		{
			name:     "same version",
			latest:   "v0.3.0",
			current:  "v0.3.0",
			expected: false,
		},
		{
			name:     "older version",
			latest:   "v0.2.0",
			current:  "v0.3.0",
			expected: false,
		},
		{
			name:     "dev version always updates",
			latest:   "v0.1.0",
			current:  "dev",
			expected: true,
		},
		{
			name:     "without v prefix",
			latest:   "0.4.0",
			current:  "0.3.0",
			expected: true,
		},
		{
			name:     "mixed v prefix",
			latest:   "v0.4.0",
			current:  "0.3.0",
			expected: true,
		},
		{
			name:     "more parts in latest",
			latest:   "v1.0.1",
			current:  "v1.0",
			expected: true,
		},
		{
			name:     "more parts in current",
			latest:   "v1.0",
			current:  "v1.0.1",
			expected: false,
		},
		{
			name:     "major version jump",
			latest:   "v2.0.0",
			current:  "v0.9.9",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNewerVersion(tt.latest, tt.current)
			if result != tt.expected {
				t.Errorf("isNewerVersion(%q, %q) = %v, expected %v",
					tt.latest, tt.current, result, tt.expected)
			}
		})
	}
}

func TestUpdateResultFields(t *testing.T) {
	result := &UpdateResult{
		CurrentVersion:  "v0.3.0",
		LatestVersion:   "v0.4.0",
		UpdateAvailable: true,
		Updated:         false,
		Error:           nil,
	}

	if result.CurrentVersion != "v0.3.0" {
		t.Errorf("CurrentVersion = %q, expected %q", result.CurrentVersion, "v0.3.0")
	}

	if result.LatestVersion != "v0.4.0" {
		t.Errorf("LatestVersion = %q, expected %q", result.LatestVersion, "v0.4.0")
	}

	if !result.UpdateAvailable {
		t.Error("UpdateAvailable should be true")
	}

	if result.Updated {
		t.Error("Updated should be false")
	}

	if result.Error != nil {
		t.Errorf("Error should be nil, got %v", result.Error)
	}
}
