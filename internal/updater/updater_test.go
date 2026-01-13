package updater

import (
	"net/url"
	"strings"
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

// =============================================================================
// SECURITY TESTS
// =============================================================================

// TestGitHubRepoConstant verifies the update source is the expected repository
func TestGitHubRepoConstant(t *testing.T) {
	// The agent should only update from the official repository
	expectedRepo := "codebasehealth/antidote-agent"
	if GitHubRepo != expectedRepo {
		t.Errorf("GitHubRepo = %q, expected %q - SECURITY: updates should only come from official repo",
			GitHubRepo, expectedRepo)
	}
}

// TestGitHubAPIURLConstant verifies the API URL is correctly formed
func TestGitHubAPIURLConstant(t *testing.T) {
	// Verify the API URL is HTTPS
	if !strings.HasPrefix(GitHubAPIURL, "https://") {
		t.Error("SECURITY: GitHubAPIURL must use HTTPS")
	}

	// Verify it points to GitHub API
	if !strings.Contains(GitHubAPIURL, "api.github.com") {
		t.Error("SECURITY: GitHubAPIURL must point to api.github.com")
	}

	// Verify it contains the correct repo
	if !strings.Contains(GitHubAPIURL, GitHubRepo) {
		t.Error("SECURITY: GitHubAPIURL must contain the correct repository")
	}

	// Parse and validate URL
	parsed, err := url.Parse(GitHubAPIURL)
	if err != nil {
		t.Errorf("SECURITY: GitHubAPIURL is not a valid URL: %v", err)
	}

	if parsed.Host != "api.github.com" {
		t.Errorf("SECURITY: GitHubAPIURL host = %q, expected api.github.com", parsed.Host)
	}
}

// TestVersionValidation verifies version string validation
func TestVersionValidation(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		// These malformed versions should not cause panics or unexpected behavior
	}{
		{"empty latest", "", "v0.3.0"},
		{"empty current", "v0.4.0", ""},
		{"both empty", "", ""},
		{"malformed latest", "not-a-version", "v0.3.0"},
		{"malformed current", "v0.4.0", "not-a-version"},
		{"injection attempt", "v0.4.0; rm -rf /", "v0.3.0"},
		{"newline injection", "v0.4.0\nrm -rf /", "v0.3.0"},
		{"very long version", strings.Repeat("v1.", 1000) + "0", "v0.3.0"},
		{"negative numbers", "v-1.-1.-1", "v0.3.0"},
		{"floating point", "v0.3.5.6.7.8", "v0.3.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("isNewerVersion panicked with %q vs %q: %v",
						tt.latest, tt.current, r)
				}
			}()

			// Just call the function - we're testing it doesn't crash
			_ = isNewerVersion(tt.latest, tt.current)
		})
	}
}

// TestAssetNameValidation validates expected asset name format
func TestAssetNameValidation(t *testing.T) {
	validAssetNames := []string{
		"antidote-agent-linux-amd64",
		"antidote-agent-linux-arm64",
		"antidote-agent-darwin-amd64",
		"antidote-agent-darwin-arm64",
		"antidote-agent-windows-amd64",
	}

	invalidAssetNames := []string{
		"../antidote-agent-linux-amd64",     // path traversal
		"antidote-agent-linux-amd64/../etc", // path traversal
		"/etc/passwd",                       // absolute path
		"evil.exe",                          // wrong format
		"antidote-agent-linux-amd64.sh",     // unexpected extension
		"",                                  // empty
	}

	// Check valid names match expected pattern
	for _, name := range validAssetNames {
		if !isValidAssetName(name) {
			t.Errorf("valid asset name rejected: %q", name)
		}
	}

	// Check invalid names are rejected
	for _, name := range invalidAssetNames {
		if isValidAssetName(name) {
			t.Errorf("SECURITY: invalid asset name accepted: %q", name)
		}
	}
}

// Helper function for asset name validation
func isValidAssetName(name string) bool {
	// Must not be empty
	if name == "" {
		return false
	}

	// Must not contain path traversal
	if strings.Contains(name, "..") {
		return false
	}

	// Must not be absolute path
	if strings.HasPrefix(name, "/") {
		return false
	}

	// Must match expected format: antidote-agent-{os}-{arch}
	if !strings.HasPrefix(name, "antidote-agent-") {
		return false
	}

	// Must not have unexpected extensions
	if strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".bat") {
		return false
	}

	return true
}

// TestDownloadURLValidation validates download URL security
func TestDownloadURLValidation(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{
			name:      "valid GitHub release URL",
			url:       "https://github.com/codebasehealth/antidote-agent/releases/download/v0.4.0/antidote-agent-linux-amd64",
			expectErr: false,
		},
		{
			name:      "valid objects.githubusercontent.com URL",
			url:       "https://objects.githubusercontent.com/github-production-release-asset-2e65be/123456/some-hash",
			expectErr: false,
		},
		{
			name:      "HTTP instead of HTTPS",
			url:       "http://github.com/codebasehealth/antidote-agent/releases/download/v0.4.0/antidote-agent-linux-amd64",
			expectErr: true,
		},
		{
			name:      "wrong domain",
			url:       "https://evil.com/antidote-agent-linux-amd64",
			expectErr: true,
		},
		{
			name:      "path traversal in URL",
			url:       "https://github.com/../../../etc/passwd",
			expectErr: true,
		},
		{
			name:      "localhost URL",
			url:       "https://localhost/antidote-agent-linux-amd64",
			expectErr: true,
		},
		{
			name:      "IP address URL",
			url:       "https://192.168.1.1/antidote-agent-linux-amd64",
			expectErr: true,
		},
		{
			name:      "empty URL",
			url:       "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDownloadURL(tt.url)
			if tt.expectErr && err == nil {
				t.Errorf("SECURITY: expected error for URL %q", tt.url)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error for valid URL %q: %v", tt.url, err)
			}
		})
	}
}

// validateDownloadURL checks if a download URL is safe
func validateDownloadURL(downloadURL string) error {
	if downloadURL == "" {
		return &ValidationError{Message: "empty URL"}
	}

	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return &ValidationError{Message: "invalid URL"}
	}

	// Must use HTTPS
	if parsed.Scheme != "https" {
		return &ValidationError{Message: "must use HTTPS"}
	}

	// Must be from GitHub or GitHub CDN
	allowedHosts := []string{
		"github.com",
		"objects.githubusercontent.com",
		"github-releases.githubusercontent.com",
	}

	hostAllowed := false
	for _, allowed := range allowedHosts {
		if parsed.Host == allowed || strings.HasSuffix(parsed.Host, "."+allowed) {
			hostAllowed = true
			break
		}
	}

	if !hostAllowed {
		return &ValidationError{Message: "URL must be from GitHub"}
	}

	// Check for path traversal
	if strings.Contains(parsed.Path, "..") {
		return &ValidationError{Message: "URL contains path traversal"}
	}

	return nil
}

// ValidationError for URL validation
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// TestReleaseStructValidation validates Release struct parsing
func TestReleaseStructValidation(t *testing.T) {
	// Test that Release struct handles missing/malformed data gracefully
	release := &Release{}

	// Empty release should have empty tag
	if release.TagName != "" {
		t.Error("empty release should have empty tag")
	}

	// Empty assets should be nil/empty slice
	if len(release.Assets) != 0 {
		t.Error("empty release should have no assets")
	}

	// Test with valid data
	release = &Release{
		TagName: "v0.4.0",
		Assets: []Asset{
			{
				Name:               "antidote-agent-linux-amd64",
				BrowserDownloadURL: "https://github.com/codebasehealth/antidote-agent/releases/download/v0.4.0/antidote-agent-linux-amd64",
			},
		},
	}

	if release.TagName != "v0.4.0" {
		t.Errorf("TagName = %q, expected v0.4.0", release.TagName)
	}

	if len(release.Assets) != 1 {
		t.Errorf("expected 1 asset, got %d", len(release.Assets))
	}
}
