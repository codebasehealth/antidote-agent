package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/config"
	"github.com/codebasehealth/antidote-agent/internal/messages"
)

// OutputHandler is called when command output is produced
type OutputHandler func(msg *messages.CommandOutputMessage)

// CompleteHandler is called when a command completes
type CompleteHandler func(msg *messages.CommandCompleteMessage)

// Pool manages command execution
type Pool struct {
	cfg             *config.Config
	outputHandler   OutputHandler
	completeHandler CompleteHandler

	running   map[string]context.CancelFunc
	runningMu sync.Mutex
}

// NewPool creates a new executor pool
func NewPool(cfg *config.Config, outputHandler OutputHandler, completeHandler CompleteHandler) *Pool {
	return &Pool{
		cfg:             cfg,
		outputHandler:   outputHandler,
		completeHandler: completeHandler,
		running:         make(map[string]context.CancelFunc),
	}
}

// Execute runs a command
func (p *Pool) Execute(cmdMsg *messages.CommandMessage) error {
	action, ok := p.cfg.GetAction(cmdMsg.Action)
	if !ok {
		// Send immediate failure
		if p.completeHandler != nil {
			p.completeHandler(messages.NewCommandCompleteMessage(cmdMsg.ID, 1, 0))
		}
		return fmt.Errorf("unknown action: %s", cmdMsg.Action)
	}

	// Create cancellable context with timeout
	timeout := time.Duration(cmdMsg.Timeout) * time.Second
	if timeout == 0 {
		timeout = action.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// Track running command
	p.runningMu.Lock()
	p.running[cmdMsg.ID] = cancel
	p.runningMu.Unlock()

	// Run in goroutine
	go func() {
		defer func() {
			cancel()
			p.runningMu.Lock()
			delete(p.running, cmdMsg.ID)
			p.runningMu.Unlock()
		}()

		p.executeAction(ctx, cmdMsg, action)
	}()

	return nil
}

// Cancel cancels a running command
func (p *Pool) Cancel(id string) bool {
	p.runningMu.Lock()
	cancel, ok := p.running[id]
	p.runningMu.Unlock()

	if ok && cancel != nil {
		cancel()
		return true
	}
	return false
}

// executeAction runs the actual command
func (p *Pool) executeAction(ctx context.Context, cmdMsg *messages.CommandMessage, action config.Action) {
	startTime := time.Now()

	// Prepare command string with parameter substitution
	cmdStr := substituteParams(action.Command, cmdMsg.Params)

	log.Printf("Executing action %s: %s", cmdMsg.Action, cmdStr)

	// Create command
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)

	// Set working directory
	if action.WorkingDir != "" {
		cmd.Dir = action.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range action.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Add params as environment variables
	for k, v := range cmdMsg.Params {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start command: %v", err)
		p.sendComplete(cmdMsg.ID, 1, startTime)
		return
	}

	// Stream output
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		p.streamOutput(cmdMsg.ID, "stdout", stdout)
	}()

	go func() {
		defer wg.Done()
		p.streamOutput(cmdMsg.ID, "stderr", stderr)
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

	p.sendComplete(cmdMsg.ID, exitCode, startTime)
}

// streamOutput reads from a reader and sends output messages
func (p *Pool) streamOutput(id, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if p.outputHandler != nil {
			p.outputHandler(messages.NewCommandOutputMessage(id, stream, line+"\n"))
		}
	}
}

// sendComplete sends a command complete message
func (p *Pool) sendComplete(id string, exitCode int, startTime time.Time) {
	durationMs := time.Since(startTime).Milliseconds()
	log.Printf("Command %s completed with exit code %d (duration: %dms)", id, exitCode, durationMs)

	if p.completeHandler != nil {
		p.completeHandler(messages.NewCommandCompleteMessage(id, exitCode, durationMs))
	}
}

// substituteParams replaces ${VAR} patterns in the command with param values
func substituteParams(cmd string, params map[string]string) string {
	result := cmd
	for k, v := range params {
		// Replace ${VAR} pattern
		result = strings.ReplaceAll(result, fmt.Sprintf("${%s}", k), v)
		// Replace ${VAR:-default} pattern (remove the default since we have a value)
		// This is a simplified version - a full implementation would handle defaults properly
	}
	return result
}
