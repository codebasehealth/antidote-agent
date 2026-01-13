package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// =============================================================================
// KEY GENERATION TESTS
// =============================================================================

func TestGenerateKeyPair(t *testing.T) {
	signer, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	// Verify key lengths
	pubKey := signer.PublicKeyBase64()
	privKey := signer.PrivateKeyBase64()

	pubBytes, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil {
		t.Fatalf("failed to decode public key: %v", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, expected %d", len(pubBytes), ed25519.PublicKeySize)
	}

	privBytes, err := base64.StdEncoding.DecodeString(privKey)
	if err != nil {
		t.Fatalf("failed to decode private key: %v", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		t.Errorf("private key size = %d, expected %d", len(privBytes), ed25519.PrivateKeySize)
	}
}

func TestNewSignerFromPrivateKey(t *testing.T) {
	// Generate a key pair first
	original, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	// Recreate signer from private key
	recreated, err := NewSignerFromPrivateKey(original.PrivateKeyBase64())
	if err != nil {
		t.Fatalf("failed to create signer from private key: %v", err)
	}

	// Public keys should match
	if original.PublicKeyBase64() != recreated.PublicKeyBase64() {
		t.Error("public keys don't match after recreating signer")
	}
}

// =============================================================================
// VERIFIER TESTS
// =============================================================================

func TestNewVerifier_ValidKey(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, err := NewVerifier(signer.PublicKeyBase64())
	if err != nil {
		t.Fatalf("failed to create verifier: %v", err)
	}
	if !verifier.IsEnabled() {
		t.Error("verifier should be enabled")
	}
}

func TestNewVerifier_EmptyKey(t *testing.T) {
	verifier, err := NewVerifier("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verifier.IsEnabled() {
		t.Error("verifier should be disabled with empty key")
	}
}

func TestNewVerifier_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"not base64", "not-valid-base64!!!"},
		{"wrong size", base64.StdEncoding.EncodeToString([]byte("tooshort"))},
		{"wrong size long", base64.StdEncoding.EncodeToString(make([]byte, 64))}, // Too long
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVerifier(tt.key)
			if err == nil {
				t.Error("expected error for invalid key")
			}
		})
	}
}

// =============================================================================
// SIGNATURE VERIFICATION TESTS
// =============================================================================

func TestVerifyCommand_ValidSignature(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	nonce := generateNonce()
	cmd := signer.CreateSignedCommand(
		"cmd_123",
		"php artisan cache:clear",
		"/var/www/app",
		map[string]string{"APP_ENV": "production"},
		60,
		nonce,
	)

	data, _ := json.Marshal(cmd)
	verified, err := verifier.VerifyCommand(data)
	if err != nil {
		t.Fatalf("verification failed: %v", err)
	}

	if verified.ID != cmd.ID {
		t.Errorf("ID mismatch: got %s, expected %s", verified.ID, cmd.ID)
	}
	if verified.Command != cmd.Command {
		t.Errorf("Command mismatch: got %s, expected %s", verified.Command, cmd.Command)
	}
}

func TestVerifyCommand_InvalidSignature(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	nonce := generateNonce()
	cmd := signer.CreateSignedCommand("cmd_123", "original command", "/var/www/app", nil, 60, nonce)

	// Tamper with the command
	cmd.Command = "tampered command"

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyCommand_WrongKey(t *testing.T) {
	signer1, _ := GenerateKeyPair()
	signer2, _ := GenerateKeyPair() // Different key pair

	verifier, _ := NewVerifier(signer2.PublicKeyBase64())

	nonce := generateNonce()
	cmd := signer1.CreateSignedCommand("cmd_123", "php artisan cache:clear", "", nil, 0, nonce)

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyCommand_MissingSignature(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Nonce:     generateNonce(),
		// Signature intentionally omitted
	}

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrMissingSignature {
		t.Errorf("expected ErrMissingSignature, got %v", err)
	}
}

func TestVerifyCommand_MissingTimestamp(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Nonce:     generateNonce(),
		Signature: "fake",
		// Timestamp intentionally omitted
	}

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrMissingTimestamp {
		t.Errorf("expected ErrMissingTimestamp, got %v", err)
	}
}

func TestVerifyCommand_MissingNonce(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Signature: "fake",
		// Nonce intentionally omitted
	}

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrMissingNonce {
		t.Errorf("expected ErrMissingNonce, got %v", err)
	}
}

// =============================================================================
// REPLAY PROTECTION TESTS
// =============================================================================

func TestVerifyCommand_ExpiredMessage(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	// Create command with old timestamp
	cmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Timestamp: time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339), // 10 minutes old
		Nonce:     generateNonce(),
	}
	cmd.Signature = signer.SignCommand(cmd)

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrMessageExpired {
		t.Errorf("expected ErrMessageExpired, got %v", err)
	}
}

func TestVerifyCommand_FutureMessage(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	// Create command with future timestamp
	cmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Timestamp: time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339), // 10 minutes in future
		Nonce:     generateNonce(),
	}
	cmd.Signature = signer.SignCommand(cmd)

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrMessageFromFuture {
		t.Errorf("expected ErrMessageFromFuture, got %v", err)
	}
}

func TestVerifyCommand_ValidWithinWindow(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	// Test various timestamps within the valid window
	validOffsets := []time.Duration{
		0,
		-1 * time.Minute,
		-4 * time.Minute,
		-10 * time.Second,
	}

	for _, offset := range validOffsets {
		cmd := &SignedCommand{
			Type:      "command",
			ID:        "cmd_123",
			Command:   "php artisan cache:clear",
			Timestamp: time.Now().UTC().Add(offset).Format(time.RFC3339),
			Nonce:     generateNonce(),
		}
		cmd.Signature = signer.SignCommand(cmd)

		data, _ := json.Marshal(cmd)
		_, err := verifier.VerifyCommand(data)
		if err != nil {
			t.Errorf("expected valid message with offset %v, got error: %v", offset, err)
		}
	}
}

// =============================================================================
// CANONICAL MESSAGE TESTS
// =============================================================================

func TestCanonicalMessage_Deterministic(t *testing.T) {
	signer, _ := GenerateKeyPair()

	// Create the same command twice
	cmd1 := &SignedCommand{
		Type:       "command",
		ID:         "cmd_123",
		Command:    "php artisan cache:clear",
		WorkingDir: "/var/www/app",
		Env:        map[string]string{"APP_ENV": "production", "DB_HOST": "localhost"},
		Timeout:    60,
		Timestamp:  "2024-01-13T12:00:00Z",
		Nonce:      "test-nonce",
	}

	cmd2 := &SignedCommand{
		Type:       "command",
		ID:         "cmd_123",
		Command:    "php artisan cache:clear",
		WorkingDir: "/var/www/app",
		Env:        map[string]string{"DB_HOST": "localhost", "APP_ENV": "production"}, // Different order
		Timeout:    60,
		Timestamp:  "2024-01-13T12:00:00Z",
		Nonce:      "test-nonce",
	}

	sig1 := signer.SignCommand(cmd1)
	sig2 := signer.SignCommand(cmd2)

	if sig1 != sig2 {
		t.Error("signatures should be identical for equivalent commands")
	}
}

func TestCanonicalMessage_DifferentFields(t *testing.T) {
	signer, _ := GenerateKeyPair()

	baseCmd := &SignedCommand{
		Type:      "command",
		ID:        "cmd_123",
		Command:   "php artisan cache:clear",
		Timestamp: "2024-01-13T12:00:00Z",
		Nonce:     "test-nonce",
	}

	// Commands with different fields should have different signatures
	variations := []*SignedCommand{
		{Type: "command", ID: "cmd_456", Command: "php artisan cache:clear", Timestamp: "2024-01-13T12:00:00Z", Nonce: "test-nonce"},
		{Type: "command", ID: "cmd_123", Command: "different command", Timestamp: "2024-01-13T12:00:00Z", Nonce: "test-nonce"},
		{Type: "command", ID: "cmd_123", Command: "php artisan cache:clear", Timestamp: "2024-01-13T12:00:00Z", Nonce: "different-nonce"},
		{Type: "command", ID: "cmd_123", Command: "php artisan cache:clear", WorkingDir: "/tmp", Timestamp: "2024-01-13T12:00:00Z", Nonce: "test-nonce"},
	}

	baseSig := signer.SignCommand(baseCmd)

	for i, variation := range variations {
		varSig := signer.SignCommand(variation)
		if varSig == baseSig {
			t.Errorf("variation %d should have different signature", i)
		}
	}
}

// =============================================================================
// DISABLED VERIFICATION TESTS
// =============================================================================

func TestVerifyCommand_DisabledVerification(t *testing.T) {
	verifier, _ := NewVerifier("") // Empty key = disabled

	// Should parse command without verification
	cmd := &SignedCommand{
		Type:    "command",
		ID:      "cmd_123",
		Command: "php artisan cache:clear",
		// No signature, timestamp, or nonce
	}

	data, _ := json.Marshal(cmd)
	verified, err := verifier.VerifyCommand(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verified.ID != cmd.ID {
		t.Error("command ID should match")
	}
}

// =============================================================================
// TAMPER DETECTION TESTS
// =============================================================================

func TestVerifyCommand_TamperedID(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := signer.CreateSignedCommand("cmd_123", "echo safe", "", nil, 0, generateNonce())
	cmd.ID = "cmd_456" // Tamper with ID

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered ID, got %v", err)
	}
}

func TestVerifyCommand_TamperedWorkingDir(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := signer.CreateSignedCommand("cmd_123", "cat config.php", "/var/www/app", nil, 0, generateNonce())
	cmd.WorkingDir = "/etc" // Tamper with working directory

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered working_dir, got %v", err)
	}
}

func TestVerifyCommand_TamperedEnv(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := signer.CreateSignedCommand("cmd_123", "printenv", "", map[string]string{"SAFE": "value"}, 0, generateNonce())
	cmd.Env["MALICIOUS"] = "injected" // Add malicious env var

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered env, got %v", err)
	}
}

func TestVerifyCommand_TamperedTimeout(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := signer.CreateSignedCommand("cmd_123", "sleep 10", "", nil, 30, generateNonce())
	cmd.Timeout = 3600 // Tamper with timeout

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered timeout, got %v", err)
	}
}

func TestVerifyCommand_TamperedTimestamp(t *testing.T) {
	signer, _ := GenerateKeyPair()
	verifier, _ := NewVerifier(signer.PublicKeyBase64())

	cmd := signer.CreateSignedCommand("cmd_123", "echo test", "", nil, 0, generateNonce())
	cmd.Timestamp = time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339) // Change timestamp

	data, _ := json.Marshal(cmd)
	_, err := verifier.VerifyCommand(data)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered timestamp, got %v", err)
	}
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func generateNonce() string {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(nonce)
}
