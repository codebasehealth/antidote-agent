package security

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/codebasehealth/antidote-agent/internal/messages"
)

// Default deny patterns that are always enforced regardless of config
// These patterns are checked against the command AFTER stripping leading comments
// NOTE: Patterns must NOT match safe uses like 'echo "rm -rf /"' or 'grep "rm" file'
var DefaultDenyPatterns = []string{
	// === rm dangerous operations ===
	// Use negative lookbehind simulation by requiring rm at start or after shell operators
	`(^|;|\||&&|\|\|)\s*rm\s+.*--no-preserve-root`,                 // rm with --no-preserve-root anywhere
	`(^|;|\||&&|\|\|)\s*rm\s+(-[a-z]*\s+)*['"]*(/|~)['"]*\s*(&|;|$|\||&&)`, // rm -rf / or ~ with any flag combo
	`(^|;|\||&&|\|\|)\s*rm\s+.*['"]*(/\*|~)['"]*`,                  // rm -rf /* or ~
	`(^|;|\||&&|\|\|)\s*rm\s+.*\$\{?HOME\}?`,                       // rm with $HOME or ${HOME}
	`(^|;|\||&&|\|\|)\s*shred\s+`,                                  // shred command (secure deletion)

	// === Filesystem destruction ===
	`(^|;|\||&&|\|\|)\s*mkfs\.`,                           // mkfs commands
	`(^|;|\||&&|\|\|)\s*dd\s+.*of=/dev/(sd|hd|nvme|vd)`,   // dd to disk devices
	`(^|;|\||&&|\|\|)\s*dd\s+.*of=/boot/`,                 // dd to boot directory
	`>\s*/dev/(sd|hd|nvme|vd)`,                            // redirect to disk devices
	`(^|;|\||&&|\|\|)\s*hdparm\s+.*--security-erase`,      // hdparm secure erase
	`(^|;|\||&&|\|\|)\s*hdparm\s+.*--make-bad-sector`,     // hdparm bad sector creation
	`(^|;|\||&&|\|\|)\s*wipefs\s+`,                        // wipefs command

	// === Permission attacks ===
	`(^|;|\||&&|\|\|)\s*chmod\s+(-[a-z]*\s+)*[0-7]{3,4}\s+['"]*(/)['"]*\s*(&|;|$)`, // chmod [mode] /
	`(^|;|\||&&|\|\|)\s*chown\s+(-[a-z]*\s+)*\S+\s+['"]*(/)['"]*\s*(&|;|$)`,        // chown ... /

	// === Fork bombs and resource exhaustion ===
	`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`,           // fork bomb: :(){:|:&};:
	`\.0\s*\(\)\s*\{\s*\.0`,                              // alternate fork bomb
	`\w+\(\)\s*\{\s*\w+\s*\|\s*\w+\s*&\s*\}\s*;\s*\w+`,   // generic function fork bomb: bomb() { bomb | bomb & }; bomb

	// === Remote code execution ===
	`curl.*\|\s*(sh|bash|zsh|ksh|dash)`,           // curl pipe to shell
	`wget.*\|\s*(sh|bash|zsh|ksh|dash)`,           // wget pipe to shell
	`base64.*-d.*\|\s*(sh|bash|zsh|ksh|dash)`,     // base64 decode pipe to shell
	`\|\s*base64.*-d.*\|\s*(sh|bash|zsh|ksh|dash)`, // piped base64 decode to shell

	// === Language-based execution of dangerous commands ===
	`(^|;|\||&&|\|\|)\s*python[23]?\s+-c\s+.*rm\s`,             // python -c with rm
	`(^|;|\||&&|\|\|)\s*python[23]?\s+-c\s+.*rmtree`,           // python -c with shutil.rmtree
	`(^|;|\||&&|\|\|)\s*python[23]?\s+-c\s+.*unlink`,           // python -c with os.unlink
	`(^|;|\||&&|\|\|)\s*perl\s+-e\s+.*rm\s`,                    // perl -e with rm
	`(^|;|\||&&|\|\|)\s*perl\s+-e\s+.*unlink`,                  // perl -e with unlink
	`(^|;|\||&&|\|\|)\s*ruby\s+-e\s+.*rm\s`,                    // ruby -e with rm
	`(^|;|\||&&|\|\|)\s*ruby\s+-e\s+.*FileUtils`,               // ruby -e with FileUtils

	// === Command substitution/injection ===
	`\$\([^)]*rm\s`,                   // $(rm ...) command substitution
	`\$\([^)]*mkfs`,                   // $(mkfs...) command substitution
	`\$\([^)]*dd\s+.*of=/dev/`,        // $(dd if=... of=/dev/...) command substitution
	"`[^`]*rm\\s",                     // `rm ...` backtick substitution
	"`[^`]*mkfs",                      // `mkfs...` backtick substitution
	"`[^`]*dd\\s+.*of=/dev/",          // `dd ...` backtick substitution
	`<\([^)]*rm\s`,                    // <(rm ...) process substitution
	`<\([^)]*dd\s+.*of=/dev/`,         // <(dd ...) process substitution

	// === Heredoc with dangerous commands ===
	`<<\s*['"]?\w*['"]?\s*\n.*rm\s+-rf`,  // heredoc containing rm -rf

	// === Background execution of dangerous commands ===
	`(^|;|\||&&|\|\|)\s*nohup\s+.*rm\s`,    // nohup rm ...
	`(^|;|\||&&|\|\|)\s*nohup\s+.*mkfs`,    // nohup mkfs ...
	`(^|;|\||&&|\|\|)\s*nohup\s+.*dd\s`,    // nohup dd ...

	// === Null device tricks ===
	`/dev/null.*>.*&`, // null redirect tricks

	// === Kernel/system manipulation ===
	`(^|;|\||&&|\|\|)\s*sysctl\s+-w`,              // sysctl write
	`(^|;|\||&&|\|\|)\s*modprobe\s+-r`,            // module removal
	`(^|;|\||&&|\|\|)\s*rmmod\s+`,                 // module removal
	`(^|;|\||&&|\|\|)\s*insmod\s+`,                // module insertion
	`echo\s+.*>\s*/proc/`,                         // writing to /proc
	`echo\s+.*>\s*/sys/`,                          // writing to /sys

	// === Network attacks ===
	`(^|;|\||&&|\|\|)\s*iptables\s+-F`,    // flush all iptables rules
	`(^|;|\||&&|\|\|)\s*iptables\s+-X`,    // delete all chains
	`(^|;|\||&&|\|\|)\s*ip\s+link\s+del`,  // delete network interfaces

	// === Password/shadow file access ===
	`(^|;|\||&&|\|\|)\s*cat\s+/etc/shadow`,   // reading shadow file
	`cp\s+.*\s+/etc/shadow`,                  // overwriting shadow file
	`>\s*/etc/shadow`,                        // truncating shadow file
}

// Critical environment variables that cannot be overridden
var ProtectedEnvVars = map[string]bool{
	"PATH":            true,
	"LD_PRELOAD":      true,
	"LD_LIBRARY_PATH": true,
	"DYLD_INSERT_LIBRARIES": true,
	"DYLD_LIBRARY_PATH":     true,
	"HOME":                  true,
	"USER":                  true,
	"SHELL":                 true,
	"IFS":                   true,
}

// Limits for command validation
const (
	MaxCommandLength = 65536   // 64KB max command length
	MaxCommandIDLen  = 256     // Max command ID length
	MaxEnvVarNameLen = 256     // Max env var name length
	MaxEnvVarValueLen = 32768  // 32KB max env var value
	MaxTimeout       = 3600    // 1 hour max timeout
)

// ValidationError represents a security validation failure
type ValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Validator validates commands before execution
type Validator struct {
	mu           sync.RWMutex
	appConfigs   map[string]*messages.AppConfig // path -> config
	allowedPaths []string                        // paths where commands can run
	denyPatterns []*regexp.Regexp                // compiled deny patterns
}

// NewValidator creates a new security validator
func NewValidator() *Validator {
	v := &Validator{
		appConfigs:   make(map[string]*messages.AppConfig),
		allowedPaths: []string{},
	}

	// Compile default deny patterns
	v.compileDenyPatterns(DefaultDenyPatterns)

	return v
}

// UpdateApps updates the validator with discovered apps
func (v *Validator) UpdateApps(apps []messages.AppInfo) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Clear existing
	v.appConfigs = make(map[string]*messages.AppConfig)
	v.allowedPaths = []string{}

	// Collect all deny patterns (default + per-app)
	allPatterns := make([]string, len(DefaultDenyPatterns))
	copy(allPatterns, DefaultDenyPatterns)

	for _, app := range apps {
		// Normalize path
		cleanPath := filepath.Clean(app.Path)
		v.allowedPaths = append(v.allowedPaths, cleanPath)

		if app.Config != nil {
			v.appConfigs[cleanPath] = app.Config

			// Add app-specific deny patterns
			for _, pattern := range app.Config.Deny {
				allPatterns = append(allPatterns, pattern)
			}
		}
	}

	// Recompile all deny patterns
	v.compileDenyPatterns(allPatterns)
}

// compileDenyPatterns compiles regex patterns
func (v *Validator) compileDenyPatterns(patterns []string) {
	v.denyPatterns = make([]*regexp.Regexp, 0, len(patterns))

	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Log but don't fail - treat invalid patterns as literal strings
			// Try escaping as literal
			escaped := regexp.QuoteMeta(pattern)
			if re, err = regexp.Compile(escaped); err != nil {
				continue
			}
		}
		v.denyPatterns = append(v.denyPatterns, re)
	}
}

// ValidateCommand checks if a command is safe to execute
func (v *Validator) ValidateCommand(cmd *messages.CommandMessage) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	// Check command ID length
	if len(cmd.ID) > MaxCommandIDLen {
		return &ValidationError{
			Code:    "COMMAND_ID_TOO_LONG",
			Message: fmt.Sprintf("command ID exceeds maximum length of %d", MaxCommandIDLen),
		}
	}

	// Check command length
	if len(cmd.Command) > MaxCommandLength {
		return &ValidationError{
			Code:    "COMMAND_TOO_LONG",
			Message: fmt.Sprintf("command exceeds maximum length of %d bytes", MaxCommandLength),
		}
	}

	// Check timeout bounds
	if cmd.Timeout > MaxTimeout {
		return &ValidationError{
			Code:    "TIMEOUT_TOO_LONG",
			Message: fmt.Sprintf("timeout exceeds maximum of %d seconds", MaxTimeout),
		}
	}

	// Validate working directory
	if cmd.WorkingDir != "" {
		if err := v.validateWorkingDir(cmd.WorkingDir); err != nil {
			return err
		}
	}

	// Validate environment variables
	if err := v.validateEnvVars(cmd.Env); err != nil {
		return err
	}

	// Check against deny patterns
	if err := v.checkDenyPatterns(cmd.Command); err != nil {
		return err
	}

	return nil
}

// validateWorkingDir ensures the working directory is within allowed paths
func (v *Validator) validateWorkingDir(dir string) error {
	cleanDir := filepath.Clean(dir)

	// Check for null bytes (path truncation attack)
	if strings.Contains(dir, "\x00") {
		return &ValidationError{
			Code:    "PATH_TRAVERSAL",
			Message: "working directory contains null byte",
		}
	}

	// Check for actual ".." path components (not "..." or "....")
	// Split by / and check each component
	if containsPathTraversal(dir) {
		return &ValidationError{
			Code:    "PATH_TRAVERSAL",
			Message: "working directory contains path traversal",
		}
	}

	// If no allowed paths configured, allow any path (legacy mode)
	if len(v.allowedPaths) == 0 {
		return nil
	}

	// Check if the directory is within an allowed path
	for _, allowed := range v.allowedPaths {
		if strings.HasPrefix(cleanDir, allowed) {
			return nil
		}
	}

	return &ValidationError{
		Code:    "INVALID_WORKING_DIR",
		Message: fmt.Sprintf("working directory %s is not within any allowed application path", dir),
	}
}

// containsPathTraversal checks if a path contains actual ".." traversal components
func containsPathTraversal(path string) bool {
	// Split path by directory separator
	parts := strings.Split(path, "/")
	for _, part := range parts {
		// Trim spaces from the part
		trimmed := strings.TrimSpace(part)

		// Check if it's exactly ".."
		if trimmed == ".." {
			return true
		}

		// Check for space-obfuscated traversal like ". ." or ".  ."
		// The pattern is: dot, one or more spaces, dot (with nothing else)
		if isSpaceObfuscatedTraversal(trimmed) {
			return true
		}
	}
	return false
}

// isSpaceObfuscatedTraversal detects patterns like ". .", ".  .", etc.
// These are attempts to bypass ".." detection using spaces
func isSpaceObfuscatedTraversal(s string) bool {
	// Must start with dot and end with dot
	if !strings.HasPrefix(s, ".") || !strings.HasSuffix(s, ".") {
		return false
	}

	// Must contain at least one space
	if !strings.Contains(s, " ") {
		return false
	}

	// Remove all dots and spaces - if nothing remains, it's traversal
	cleaned := strings.ReplaceAll(s, ".", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	return cleaned == ""
}

// validateEnvVars checks environment variables for security issues
func (v *Validator) validateEnvVars(env map[string]string) error {
	for name, value := range env {
		// Check name length
		if len(name) > MaxEnvVarNameLen {
			return &ValidationError{
				Code:    "ENV_NAME_TOO_LONG",
				Message: fmt.Sprintf("environment variable name exceeds maximum length of %d", MaxEnvVarNameLen),
			}
		}

		// Check value length
		if len(value) > MaxEnvVarValueLen {
			return &ValidationError{
				Code:    "ENV_VALUE_TOO_LONG",
				Message: fmt.Sprintf("environment variable value exceeds maximum length of %d", MaxEnvVarValueLen),
			}
		}

		// Check for protected variables
		upperName := strings.ToUpper(name)
		if ProtectedEnvVars[upperName] {
			return &ValidationError{
				Code:    "PROTECTED_ENV_VAR",
				Message: fmt.Sprintf("cannot override protected environment variable: %s", name),
			}
		}

		// Check for null bytes or other control characters in name
		if strings.ContainsAny(name, "\x00=") {
			return &ValidationError{
				Code:    "INVALID_ENV_NAME",
				Message: fmt.Sprintf("environment variable name contains invalid characters: %s", name),
			}
		}
	}

	return nil
}

// checkDenyPatterns checks if command matches any deny pattern
func (v *Validator) checkDenyPatterns(command string) error {
	trimmedCmd := strings.TrimSpace(command)

	// Skip pure comment lines - they're not executable
	if strings.HasPrefix(trimmedCmd, "#") {
		return nil
	}

	// Split by newlines and check each line (handles newline injection)
	lines := strings.Split(trimmedCmd, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments for checking (everything after unquoted #)
		cmdToCheck := stripInlineComments(line)
		if cmdToCheck == "" {
			continue
		}

		// Also check lowercase version for case-insensitive patterns
		normalizedCmd := strings.ToLower(cmdToCheck)

		for _, pattern := range v.denyPatterns {
			if pattern.MatchString(cmdToCheck) || pattern.MatchString(normalizedCmd) {
				return &ValidationError{
					Code:    "COMMAND_DENIED",
					Message: fmt.Sprintf("command matches denied pattern: %s", pattern.String()),
				}
			}
		}
	}

	return nil
}

// stripInlineComments removes comments that appear after the command
// but preserves # inside quotes
func stripInlineComments(cmd string) string {
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for i, ch := range cmd {
		if escaped {
			escaped = false
			continue
		}

		switch ch {
		case '\\':
			escaped = true
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return strings.TrimSpace(cmd[:i])
			}
		}
	}

	return cmd
}

// GetAppConfig returns the config for a given path
func (v *Validator) GetAppConfig(path string) *messages.AppConfig {
	v.mu.RLock()
	defer v.mu.RUnlock()

	cleanPath := filepath.Clean(path)

	// Check exact match first
	if config, ok := v.appConfigs[cleanPath]; ok {
		return config
	}

	// Check if path is within an app directory
	for appPath, config := range v.appConfigs {
		if strings.HasPrefix(cleanPath, appPath) {
			return config
		}
	}

	return nil
}

// AllowedPaths returns the list of allowed working directories
func (v *Validator) AllowedPaths() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	paths := make([]string, len(v.allowedPaths))
	copy(paths, v.allowedPaths)
	return paths
}
