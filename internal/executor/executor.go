package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/codebasehealth/antidote-agent/internal/security"
)

const DefaultTimeout = 5 * time.Minute

// OutputHandler is called when command output is produced
type OutputHandler func(msg *messages.OutputMessage)

// CompleteHandler is called when a command completes
type CompleteHandler func(msg *messages.CompleteMessage)

// RejectedHandler is called when a command is rejected by security validation
type RejectedHandler func(msg *messages.RejectedMessage)

// Executor manages command execution
type Executor struct {
	outputHandler   OutputHandler
	completeHandler CompleteHandler
	rejectedHandler RejectedHandler
	validator       *security.Validator

	running   map[string]context.CancelFunc
	runningMu sync.Mutex
}

// New creates a new executor
func New(outputHandler OutputHandler, completeHandler CompleteHandler, rejectedHandler RejectedHandler, validator *security.Validator) *Executor {
	return &Executor{
		outputHandler:   outputHandler,
		completeHandler: completeHandler,
		rejectedHandler: rejectedHandler,
		validator:       validator,
		running:         make(map[string]context.CancelFunc),
	}
}

// Execute runs a command from the cloud
func (e *Executor) Execute(cmdMsg *messages.CommandMessage) error {
	// Security validation
	if e.validator != nil {
		if err := e.validator.ValidateCommand(cmdMsg); err != nil {
			log.Printf("Command %s rejected: %v", cmdMsg.ID, err)

			// Send rejection message back to cloud
			if e.rejectedHandler != nil {
				code := "VALIDATION_ERROR"
				if vErr, ok := err.(*security.ValidationError); ok {
					code = vErr.Code
				}
				e.rejectedHandler(messages.NewRejectedMessage(cmdMsg.ID, code, err.Error()))
			}

			return err
		}
	}

	// Determine timeout
	timeout := DefaultTimeout
	if cmdMsg.Timeout > 0 {
		timeout = time.Duration(cmdMsg.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// Track running command
	e.runningMu.Lock()
	e.running[cmdMsg.ID] = cancel
	e.runningMu.Unlock()

	// Run in goroutine
	go func() {
		defer func() {
			cancel()
			e.runningMu.Lock()
			delete(e.running, cmdMsg.ID)
			e.runningMu.Unlock()
		}()

		e.executeCommand(ctx, cmdMsg)
	}()

	return nil
}

// UpdateValidator updates the security validator with new app configs
func (e *Executor) UpdateValidator(apps []messages.AppInfo) {
	if e.validator != nil {
		e.validator.UpdateApps(apps)
		log.Printf("Security validator updated with %d apps", len(apps))
	}
}

// Cancel cancels a running command
func (e *Executor) Cancel(id string) bool {
	e.runningMu.Lock()
	cancel, ok := e.running[id]
	e.runningMu.Unlock()

	if ok && cancel != nil {
		cancel()
		return true
	}
	return false
}

// executeCommand runs the actual shell command
func (e *Executor) executeCommand(ctx context.Context, cmdMsg *messages.CommandMessage) {
	startTime := time.Now()

	log.Printf("Executing command %s: %s", cmdMsg.ID, cmdMsg.Command)

	// Create command
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdMsg.Command)

	// Set working directory
	if cmdMsg.WorkingDir != "" {
		cmd.Dir = cmdMsg.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range cmdMsg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create stdout pipe: %v", err)
		e.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to create stderr pipe: %v", err)
		e.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start command: %v", err)
		e.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	// Stream output
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		e.streamOutput(cmdMsg.ID, "stdout", stdout)
	}()

	go func() {
		defer wg.Done()
		e.streamOutput(cmdMsg.ID, "stderr", stderr)
	}()

	// Wait for output streaming to complete
	wg.Wait()

	// Wait for command to finish
	err = cmd.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = 124 // Timeout exit code
			log.Printf("Command timed out")
		} else {
			exitCode = 1
		}
	}

	e.sendComplete(cmdMsg.ID, exitCode, startTime)
}

// streamOutput reads from a reader and sends output messages
func (e *Executor) streamOutput(id, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if e.outputHandler != nil {
			e.outputHandler(messages.NewOutputMessage(id, stream, line+"\n"))
		}
	}
}

// sendComplete sends a command complete message
func (e *Executor) sendComplete(id string, exitCode int, startTime time.Time) {
	durationMs := time.Since(startTime).Milliseconds()
	log.Printf("Command %s completed with exit code %d (duration: %dms)", id, exitCode, durationMs)

	if e.completeHandler != nil {
		e.completeHandler(messages.NewCompleteMessage(id, exitCode, durationMs))
	}
}
