package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/codebasehealth/antidote-agent/internal/connection"
)

const (
	GitHubRepo   = "codebasehealth/antidote-agent"
	GitHubAPIURL = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
)

// Release represents a GitHub release
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateResult contains the result of an update check or update
type UpdateResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	Updated         bool
	Error           error
}

// CheckForUpdate checks if a newer version is available
func CheckForUpdate() (*UpdateResult, error) {
	result := &UpdateResult{
		CurrentVersion: connection.Version,
	}

	release, err := fetchLatestRelease()
	if err != nil {
		result.Error = err
		return result, err
	}

	result.LatestVersion = release.TagName
	result.UpdateAvailable = isNewerVersion(release.TagName, connection.Version)

	return result, nil
}

// SelfUpdate downloads and installs the latest version
func SelfUpdate() (*UpdateResult, error) {
	result := &UpdateResult{
		CurrentVersion: connection.Version,
	}

	// Fetch latest release info
	release, err := fetchLatestRelease()
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch latest release: %w", err)
		return result, result.Error
	}

	result.LatestVersion = release.TagName
	result.UpdateAvailable = isNewerVersion(release.TagName, connection.Version)

	if !result.UpdateAvailable {
		return result, nil
	}

	// Find the asset for current OS/arch
	assetName := fmt.Sprintf("antidote-agent-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		result.Error = fmt.Errorf("no binary found for %s/%s", runtime.GOOS, runtime.GOARCH)
		return result, result.Error
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		result.Error = fmt.Errorf("failed to get executable path: %w", err)
		return result, result.Error
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to resolve executable path: %w", err)
		return result, result.Error
	}

	// Download to temp file
	tempFile, err := downloadToTemp(downloadURL)
	if err != nil {
		result.Error = fmt.Errorf("failed to download update: %w", err)
		return result, result.Error
	}
	defer os.Remove(tempFile)

	// Make executable
	if err := os.Chmod(tempFile, 0755); err != nil {
		result.Error = fmt.Errorf("failed to make update executable: %w", err)
		return result, result.Error
	}

	// Backup current binary
	backupPath := execPath + ".backup"
	if err := os.Rename(execPath, backupPath); err != nil {
		result.Error = fmt.Errorf("failed to backup current binary: %w", err)
		return result, result.Error
	}

	// Move new binary into place
	if err := copyFile(tempFile, execPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, execPath)
		result.Error = fmt.Errorf("failed to install update: %w", err)
		return result, result.Error
	}

	// Make new binary executable
	if err := os.Chmod(execPath, 0755); err != nil {
		// Restore backup on failure
		os.Remove(execPath)
		os.Rename(backupPath, execPath)
		result.Error = fmt.Errorf("failed to set permissions: %w", err)
		return result, result.Error
	}

	// Remove backup
	os.Remove(backupPath)

	result.Updated = true
	return result, nil
}

// RestartService attempts to restart the antidote-agent systemd service
func RestartService() error {
	cmd := exec.Command("systemctl", "restart", "antidote-agent")
	return cmd.Run()
}

func fetchLatestRelease() (*Release, error) {
	resp, err := http.Get(GitHubAPIURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func downloadToTemp(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tempFile, err := os.CreateTemp("", "antidote-agent-update-*")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)
	return err
}

// isNewerVersion compares two semantic versions (e.g., "v0.3.0" vs "v0.2.0")
func isNewerVersion(latest, current string) bool {
	// Strip 'v' prefix
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	// Handle "dev" version - always update
	if current == "dev" {
		return true
	}

	// Parse versions
	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	// Compare each part
	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		var latestNum, currentNum int
		fmt.Sscanf(latestParts[i], "%d", &latestNum)
		fmt.Sscanf(currentParts[i], "%d", &currentNum)

		if latestNum > currentNum {
			return true
		}
		if latestNum < currentNum {
			return false
		}
	}

	// If latest has more parts (e.g., 1.0.1 vs 1.0), latest is newer
	return len(latestParts) > len(currentParts)
}
