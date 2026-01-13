package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAntidoteConfig(t *testing.T) {
	// Create a temp directory for test files
	tempDir, err := os.MkdirTemp("", "antidote-discovery-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name           string
		content        string
		expectNil      bool
		expectedName   string
		expectedFW     string
		expectedDeny   int
		expectedAction int
	}{
		{
			name: "valid laravel config",
			content: `version: 1
app:
  name: myapp
  framework: laravel
trust_level: balanced
actions:
  clear_cache:
    command: php artisan cache:clear
    label: Clear Cache
deny:
  - rm -rf /
  - DROP DATABASE
`,
			expectNil:      false,
			expectedName:   "myapp",
			expectedFW:     "laravel",
			expectedDeny:   2,
			expectedAction: 1,
		},
		{
			name: "valid rails config",
			content: `version: 1
app:
  name: railsapp
  framework: rails
trust_level: strict
`,
			expectNil:    false,
			expectedName: "railsapp",
			expectedFW:   "rails",
		},
		{
			name: "missing name",
			content: `version: 1
app:
  framework: laravel
`,
			expectNil: true,
		},
		{
			name: "missing framework",
			content: `version: 1
app:
  name: myapp
`,
			expectNil: true,
		},
		{
			name:      "invalid yaml",
			content:   `this is not: valid: yaml: [`,
			expectNil: true,
		},
		{
			name:      "empty file",
			content:   "",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test config file
			configPath := filepath.Join(tempDir, tt.name+".yml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			config := readAntidoteConfig(configPath)

			if tt.expectNil {
				if config != nil {
					t.Errorf("Expected nil config, got %+v", config)
				}
				return
			}

			if config == nil {
				t.Fatal("Expected non-nil config, got nil")
			}

			if config.App.Name != tt.expectedName {
				t.Errorf("Name = %q, expected %q", config.App.Name, tt.expectedName)
			}

			if config.App.Framework != tt.expectedFW {
				t.Errorf("Framework = %q, expected %q", config.App.Framework, tt.expectedFW)
			}

			if tt.expectedDeny > 0 && len(config.Deny) != tt.expectedDeny {
				t.Errorf("Deny count = %d, expected %d", len(config.Deny), tt.expectedDeny)
			}

			if tt.expectedAction > 0 && len(config.Actions) != tt.expectedAction {
				t.Errorf("Actions count = %d, expected %d", len(config.Actions), tt.expectedAction)
			}
		})
	}
}

func TestReadAntidoteConfigNotFound(t *testing.T) {
	config := readAntidoteConfig("/nonexistent/path/antidote.yml")
	if config != nil {
		t.Errorf("Expected nil for nonexistent file, got %+v", config)
	}
}

func TestAnalyzeApp(t *testing.T) {
	// Create temp directories for test apps
	tempDir, err := os.MkdirTemp("", "antidote-app-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name             string
		setupFunc        func(appDir string) error
		expectedFW       string
		expectNil        bool
		expectHasConfig  bool
	}{
		{
			name: "laravel app with artisan",
			setupFunc: func(appDir string) error {
				return os.WriteFile(filepath.Join(appDir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
			},
			expectedFW: "laravel",
		},
		{
			name: "rails app with Gemfile and routes",
			setupFunc: func(appDir string) error {
				if err := os.WriteFile(filepath.Join(appDir, "Gemfile"), []byte("gem 'rails'"), 0644); err != nil {
					return err
				}
				return nil
			},
			expectedFW: "rails",
		},
		{
			name: "django app with manage.py",
			setupFunc: func(appDir string) error {
				return os.WriteFile(filepath.Join(appDir, "manage.py"), []byte("#!/usr/bin/env python"), 0644)
			},
			expectedFW: "django",
		},
		{
			name: "go app with go.mod",
			setupFunc: func(appDir string) error {
				return os.WriteFile(filepath.Join(appDir, "go.mod"), []byte("module test"), 0644)
			},
			expectedFW: "go",
		},
		{
			name: "node app with package.json",
			setupFunc: func(appDir string) error {
				return os.WriteFile(filepath.Join(appDir, "package.json"), []byte("{}"), 0644)
			},
			expectedFW: "node",
		},
		{
			name: "nextjs app with next.config.js",
			setupFunc: func(appDir string) error {
				if err := os.WriteFile(filepath.Join(appDir, "package.json"), []byte("{}"), 0644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(appDir, "next.config.js"), []byte("module.exports = {}"), 0644)
			},
			expectedFW: "nextjs",
		},
		{
			name: "nuxt app with nuxt.config.ts",
			setupFunc: func(appDir string) error {
				if err := os.WriteFile(filepath.Join(appDir, "package.json"), []byte("{}"), 0644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(appDir, "nuxt.config.ts"), []byte("export default {}"), 0644)
			},
			expectedFW: "nuxt",
		},
		{
			name: "app with antidote.yml takes priority",
			setupFunc: func(appDir string) error {
				// Create artisan file (would be detected as Laravel)
				if err := os.WriteFile(filepath.Join(appDir, "artisan"), []byte(""), 0644); err != nil {
					return err
				}
				// But antidote.yml says rails
				config := `version: 1
app:
  name: testapp
  framework: rails
`
				return os.WriteFile(filepath.Join(appDir, "antidote.yml"), []byte(config), 0644)
			},
			expectedFW:      "rails", // antidote.yml overrides auto-detection
			expectHasConfig: true,
		},
		{
			name: "empty directory not recognized",
			setupFunc: func(appDir string) error {
				return nil // empty directory
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test app directory
			appDir := filepath.Join(tempDir, tt.name)
			if err := os.MkdirAll(appDir, 0755); err != nil {
				t.Fatalf("Failed to create app dir: %v", err)
			}

			// Run setup function
			if tt.setupFunc != nil {
				if err := tt.setupFunc(appDir); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			app := analyzeApp(appDir)

			if tt.expectNil {
				if app != nil {
					t.Errorf("Expected nil app, got framework=%q", app.Framework)
				}
				return
			}

			if app == nil {
				t.Fatal("Expected non-nil app, got nil")
			}

			if app.Framework != tt.expectedFW {
				t.Errorf("Framework = %q, expected %q", app.Framework, tt.expectedFW)
			}

			if app.Path != appDir {
				t.Errorf("Path = %q, expected %q", app.Path, appDir)
			}

			if tt.expectHasConfig && app.Config == nil {
				t.Error("Expected config to be set, got nil")
			}
		})
	}
}
