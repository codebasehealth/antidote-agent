package messages

import (
	"encoding/json"
	"testing"
)

func TestNewDiscoveryMessage(t *testing.T) {
	msg := NewDiscoveryMessage()

	if msg.Type != TypeDiscovery {
		t.Errorf("Type = %q, expected %q", msg.Type, TypeDiscovery)
	}
}

func TestNewOutputMessage(t *testing.T) {
	msg := NewOutputMessage("cmd123", "stdout", "Hello World")

	if msg.Type != TypeOutput {
		t.Errorf("Type = %q, expected %q", msg.Type, TypeOutput)
	}
	if msg.ID != "cmd123" {
		t.Errorf("ID = %q, expected %q", msg.ID, "cmd123")
	}
	if msg.Stream != "stdout" {
		t.Errorf("Stream = %q, expected %q", msg.Stream, "stdout")
	}
	if msg.Data != "Hello World" {
		t.Errorf("Data = %q, expected %q", msg.Data, "Hello World")
	}
	if msg.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

func TestNewCompleteMessage(t *testing.T) {
	msg := NewCompleteMessage("cmd123", 0, 1500)

	if msg.Type != TypeComplete {
		t.Errorf("Type = %q, expected %q", msg.Type, TypeComplete)
	}
	if msg.ID != "cmd123" {
		t.Errorf("ID = %q, expected %q", msg.ID, "cmd123")
	}
	if msg.ExitCode != 0 {
		t.Errorf("ExitCode = %d, expected 0", msg.ExitCode)
	}
	if msg.DurationMs != 1500 {
		t.Errorf("DurationMs = %d, expected 1500", msg.DurationMs)
	}
}

func TestNewHealthMessage(t *testing.T) {
	msg := NewHealthMessage(25.5, 1024, 4096, 500, 1000, 0.75)

	if msg.Type != TypeHealth {
		t.Errorf("Type = %q, expected %q", msg.Type, TypeHealth)
	}
	if msg.CPUPercent != 25.5 {
		t.Errorf("CPUPercent = %f, expected 25.5", msg.CPUPercent)
	}
	if msg.MemoryUsed != 1024 {
		t.Errorf("MemoryUsed = %d, expected 1024", msg.MemoryUsed)
	}
	if msg.MemoryTotal != 4096 {
		t.Errorf("MemoryTotal = %d, expected 4096", msg.MemoryTotal)
	}
}

func TestNewHeartbeatMessage(t *testing.T) {
	msg := NewHeartbeatMessage()

	if msg.Type != TypeHeartbeat {
		t.Errorf("Type = %q, expected %q", msg.Type, TypeHeartbeat)
	}
	if msg.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

func TestParseCommandMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectCmd   string
		expectDir   string
	}{
		{
			name:      "valid command message",
			input:     `{"type":"command","id":"cmd123","command":"php artisan cache:clear","working_dir":"/var/www/app"}`,
			expectCmd: "php artisan cache:clear",
			expectDir: "/var/www/app",
		},
		{
			name:      "command without working dir",
			input:     `{"type":"command","id":"cmd456","command":"ls -la"}`,
			expectCmd: "ls -la",
			expectDir: "",
		},
		{
			name:        "invalid json",
			input:       `{invalid}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseCommandMessage([]byte(tt.input))

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if msg.Command != tt.expectCmd {
				t.Errorf("Command = %q, expected %q", msg.Command, tt.expectCmd)
			}

			if msg.WorkingDir != tt.expectDir {
				t.Errorf("WorkingDir = %q, expected %q", msg.WorkingDir, tt.expectDir)
			}
		})
	}
}

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectType  string
		expectError bool
	}{
		{
			name:       "auth message",
			input:      `{"type":"auth","token":"abc123"}`,
			expectType: TypeAuth,
		},
		{
			name:       "command message",
			input:      `{"type":"command","id":"123","command":"ls"}`,
			expectType: TypeCommand,
		},
		{
			name:       "discover message",
			input:      `{"type":"discover"}`,
			expectType: TypeDiscover,
		},
		{
			name:        "invalid json",
			input:       `not json`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgType, err := ParseMessage([]byte(tt.input))

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if msgType != tt.expectType {
				t.Errorf("Type = %q, expected %q", msgType, tt.expectType)
			}
		})
	}
}

func TestAppConfigSerialization(t *testing.T) {
	config := &AppConfig{
		Version: 1,
		App: AppConfigApp{
			Name:      "testapp",
			Framework: "laravel",
		},
		TrustLevel: "balanced",
		Actions: map[string]AppConfigAction{
			"clear_cache": {
				Command: "php artisan cache:clear",
				Label:   "Clear Cache",
				Icon:    "trash",
				Confirm: false,
			},
		},
		Deny: []string{"rm -rf /", "DROP DATABASE"},
		Logs: []string{"storage/logs/laravel.log"},
		Health: &AppConfigHealth{
			Endpoint: "/up",
			Interval: "30s",
		},
	}

	// Serialize to JSON
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Deserialize back
	var parsed AppConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if parsed.App.Name != "testapp" {
		t.Errorf("App.Name = %q, expected %q", parsed.App.Name, "testapp")
	}

	if parsed.App.Framework != "laravel" {
		t.Errorf("App.Framework = %q, expected %q", parsed.App.Framework, "laravel")
	}

	if len(parsed.Actions) != 1 {
		t.Errorf("Actions count = %d, expected 1", len(parsed.Actions))
	}

	if action, ok := parsed.Actions["clear_cache"]; ok {
		if action.Command != "php artisan cache:clear" {
			t.Errorf("Action command = %q, expected %q", action.Command, "php artisan cache:clear")
		}
	} else {
		t.Error("Expected 'clear_cache' action to exist")
	}

	if len(parsed.Deny) != 2 {
		t.Errorf("Deny count = %d, expected 2", len(parsed.Deny))
	}

	if parsed.Health == nil {
		t.Error("Health should not be nil")
	} else if parsed.Health.Endpoint != "/up" {
		t.Errorf("Health.Endpoint = %q, expected %q", parsed.Health.Endpoint, "/up")
	}
}

func TestAppInfoWithConfig(t *testing.T) {
	app := &AppInfo{
		Path:      "/var/www/app",
		Framework: "laravel",
		GitRemote: "git@github.com:user/repo.git",
		GitBranch: "main",
		GitCommit: "abc123",
		Config: &AppConfig{
			Version: 1,
			App: AppConfigApp{
				Name:      "myapp",
				Framework: "laravel",
			},
		},
	}

	data, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed AppInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.Config == nil {
		t.Fatal("Config should not be nil")
	}

	if parsed.Config.App.Name != "myapp" {
		t.Errorf("Config.App.Name = %q, expected %q", parsed.Config.App.Name, "myapp")
	}
}
