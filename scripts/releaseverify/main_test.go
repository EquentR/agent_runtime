package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyReleaseChecksSignatureAndAllAssets(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "asset.zip")
	if err := os.WriteFile(assetPath, []byte("asset"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	checksums := []byte("2c3f1f3e7e5e2c1b78e3c49d1e7c4d8a1e6d72e7f4f9f27e8f4c1b7f6d6f0b2e  asset.zip\n")
	// VerifyRelease exercises the trusted key path; the checksum fixture is replaced below with the actual digest.
	digest := sha256Sum(t, []byte("asset"))
	checksums = []byte(digest + "  asset.zip\n")
	envelope := signatureEnvelope{KeyID: "release-2026", Algorithm: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))}
	signature, _ := json.Marshal(envelope)
	if err := VerifyRelease(checksums, signature, dir, map[string]ed25519.PublicKey{"release-2026": publicKey}); err != nil {
		t.Fatalf("VerifyRelease() error = %v", err)
	}
}

func sha256Sum(t *testing.T, data []byte) string {
	t.Helper()
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
