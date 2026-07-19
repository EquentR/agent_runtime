package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifySignedChecksumsAcceptsTrustedEd25519Signature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	checksums := []byte(strings.Repeat("a", 64) + "  ice_art_windows_amd64.zip\n")
	envelope, err := json.Marshal(SignatureEnvelope{
		KeyID:     "release-2026",
		Algorithm: "ed25519",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums)),
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if err := VerifySignedChecksums(checksums, envelope, map[string]ed25519.PublicKey{"release-2026": publicKey}); err != nil {
		t.Fatalf("VerifySignedChecksums() error = %v", err)
	}
}

func TestVerifySignedChecksumsRejectsUnknownKeyAndTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	checksums := []byte(strings.Repeat("a", 64) + "  asset.zip\n")
	envelope, err := json.Marshal(SignatureEnvelope{
		KeyID:     "other-key",
		Algorithm: "ed25519",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums)),
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := VerifySignedChecksums(checksums, envelope, map[string]ed25519.PublicKey{"release-2026": publicKey}); err == nil {
		t.Fatal("VerifySignedChecksums() error = nil, want unknown key rejection")
	}

	validEnvelope, _ := json.Marshal(SignatureEnvelope{
		KeyID:     "release-2026",
		Algorithm: "ed25519",
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums)),
	})
	if err := VerifySignedChecksums(append(checksums, 'x'), validEnvelope, map[string]ed25519.PublicKey{"release-2026": publicKey}); err == nil {
		t.Fatal("VerifySignedChecksums() error = nil, want tampering rejection")
	}
}

func TestParseChecksumsAndVerifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.zip")
	if err := os.WriteFile(path, []byte("release asset"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	checksums, err := ParseChecksums([]byte("e6abe9df7db8513616674b02b5edb26c37bf3b2f81daeec1e3c6fc8c9a802850  asset.zip\n"))
	if err != nil {
		t.Fatalf("ParseChecksums() error = %v", err)
	}
	if err := VerifyFileChecksum(path, checksums["asset.zip"]); err != nil {
		t.Fatalf("VerifyFileChecksum() error = %v", err)
	}
	if err := VerifyFileChecksum(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("VerifyFileChecksum() error = nil, want mismatch")
	}
}
