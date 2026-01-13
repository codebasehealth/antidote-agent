package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// MaxMessageAge is the maximum age of a signed message before it's rejected
	MaxMessageAge = 5 * time.Minute

	// NonceLength is the expected length of the nonce
	NonceLength = 32
)

var (
	ErrMissingSignature   = errors.New("message signature is missing")
	ErrInvalidSignature   = errors.New("message signature is invalid")
	ErrMissingTimestamp   = errors.New("message timestamp is missing")
	ErrMessageExpired     = errors.New("message has expired (replay protection)")
	ErrMessageFromFuture  = errors.New("message timestamp is in the future")
	ErrMissingNonce       = errors.New("message nonce is missing")
	ErrInvalidPublicKey   = errors.New("invalid public key format")
	ErrSigningDisabled    = errors.New("message signing is disabled")
)

// Verifier verifies signed messages from the server
type Verifier struct {
	publicKey ed25519.PublicKey
	enabled   bool
}

// NewVerifier creates a new signature verifier with the given public key
// publicKeyBase64 should be the base64-encoded Ed25519 public key
func NewVerifier(publicKeyBase64 string) (*Verifier, error) {
	if publicKeyBase64 == "" {
		// Signing disabled - return a disabled verifier
		return &Verifier{enabled: false}, nil
	}

	keyBytes, err := base64.StdEncoding.DecodeString(publicKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}

	if len(keyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d",
			ErrInvalidPublicKey, ed25519.PublicKeySize, len(keyBytes))
	}

	return &Verifier{
		publicKey: ed25519.PublicKey(keyBytes),
		enabled:   true,
	}, nil
}

// IsEnabled returns whether signature verification is enabled
func (v *Verifier) IsEnabled() bool {
	return v.enabled
}

// SignedCommand represents a command message with signature fields
type SignedCommand struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Command    string            `json:"command"`
	WorkingDir string            `json:"working_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Timeout    int               `json:"timeout,omitempty"`
	Timestamp  string            `json:"timestamp"`
	Nonce      string            `json:"nonce"`
	Signature  string            `json:"signature"`
}

// VerifyCommand verifies the signature on a command message
func (v *Verifier) VerifyCommand(data []byte) (*SignedCommand, error) {
	if !v.enabled {
		// Parse without verification when signing is disabled
		var cmd SignedCommand
		if err := json.Unmarshal(data, &cmd); err != nil {
			return nil, err
		}
		return &cmd, nil
	}

	var cmd SignedCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}

	// Validate required fields
	if cmd.Signature == "" {
		return nil, ErrMissingSignature
	}
	if cmd.Timestamp == "" {
		return nil, ErrMissingTimestamp
	}
	if cmd.Nonce == "" {
		return nil, ErrMissingNonce
	}

	// Validate timestamp (replay protection)
	if err := v.validateTimestamp(cmd.Timestamp); err != nil {
		return nil, err
	}

	// Verify signature
	if err := v.verifySignature(&cmd); err != nil {
		return nil, err
	}

	return &cmd, nil
}

// validateTimestamp checks if the message timestamp is within acceptable bounds
func (v *Verifier) validateTimestamp(timestamp string) error {
	msgTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	now := time.Now().UTC()
	age := now.Sub(msgTime)

	// Reject messages from the future (with small tolerance for clock skew)
	if age < -30*time.Second {
		return ErrMessageFromFuture
	}

	// Reject messages older than MaxMessageAge
	if age > MaxMessageAge {
		return ErrMessageExpired
	}

	return nil
}

// verifySignature verifies the Ed25519 signature on the command
func (v *Verifier) verifySignature(cmd *SignedCommand) error {
	// Decode the signature
	signature, err := base64.StdEncoding.DecodeString(cmd.Signature)
	if err != nil {
		return fmt.Errorf("%w: failed to decode signature", ErrInvalidSignature)
	}

	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: invalid signature length", ErrInvalidSignature)
	}

	// Create the canonical message to verify
	canonicalMessage := v.createCanonicalMessage(cmd)

	// Verify the signature
	if !ed25519.Verify(v.publicKey, []byte(canonicalMessage), signature) {
		return ErrInvalidSignature
	}

	return nil
}

// createCanonicalMessage creates a deterministic string representation of the command
// This ensures the same message always produces the same bytes for signing
func (v *Verifier) createCanonicalMessage(cmd *SignedCommand) string {
	// Build canonical form: sorted key=value pairs separated by newlines
	parts := []string{
		fmt.Sprintf("command=%s", cmd.Command),
		fmt.Sprintf("id=%s", cmd.ID),
		fmt.Sprintf("nonce=%s", cmd.Nonce),
		fmt.Sprintf("timestamp=%s", cmd.Timestamp),
		fmt.Sprintf("type=%s", cmd.Type),
	}

	if cmd.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("working_dir=%s", cmd.WorkingDir))
	}

	if cmd.Timeout > 0 {
		parts = append(parts, fmt.Sprintf("timeout=%d", cmd.Timeout))
	}

	// Add env vars in sorted order
	if len(cmd.Env) > 0 {
		envKeys := make([]string, 0, len(cmd.Env))
		for k := range cmd.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)

		for _, k := range envKeys {
			parts = append(parts, fmt.Sprintf("env.%s=%s", k, cmd.Env[k]))
		}
	}

	// Sort all parts for deterministic ordering
	sort.Strings(parts)

	return strings.Join(parts, "\n")
}

// =============================================================================
// SIGNER (for testing and potential future use)
// =============================================================================

// Signer signs messages (used for testing and key generation)
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// GenerateKeyPair generates a new Ed25519 key pair
func GenerateKeyPair() (*Signer, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}

	return &Signer{
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// NewSignerFromPrivateKey creates a signer from an existing private key
func NewSignerFromPrivateKey(privateKeyBase64 string) (*Signer, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}

	if len(keyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: expected %d, got %d",
			ed25519.PrivateKeySize, len(keyBytes))
	}

	privateKey := ed25519.PrivateKey(keyBytes)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return &Signer{
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// PublicKeyBase64 returns the base64-encoded public key
func (s *Signer) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

// PrivateKeyBase64 returns the base64-encoded private key
func (s *Signer) PrivateKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.privateKey)
}

// SignCommand signs a command and returns the signature
func (s *Signer) SignCommand(cmd *SignedCommand) string {
	// Use the same canonical message format as verification
	v := &Verifier{publicKey: s.publicKey, enabled: true}
	canonicalMessage := v.createCanonicalMessage(cmd)

	signature := ed25519.Sign(s.privateKey, []byte(canonicalMessage))
	return base64.StdEncoding.EncodeToString(signature)
}

// CreateSignedCommand creates a complete signed command
func (s *Signer) CreateSignedCommand(id, command, workingDir string, env map[string]string, timeout int, nonce string) *SignedCommand {
	cmd := &SignedCommand{
		Type:       "command",
		ID:         id,
		Command:    command,
		WorkingDir: workingDir,
		Env:        env,
		Timeout:    timeout,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Nonce:      nonce,
	}

	cmd.Signature = s.SignCommand(cmd)
	return cmd
}
