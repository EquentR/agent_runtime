package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestSignChecksumsProducesVerifiableEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	checksums := []byte("abc  asset.zip\n")
	raw, err := SignChecksums(checksums, "release-2026", base64.StdEncoding.EncodeToString(privateKey))
	if err != nil {
		t.Fatalf("SignChecksums() error = %v", err)
	}
	var envelope signatureEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if envelope.KeyID != "release-2026" || envelope.Algorithm != "ed25519" || !ed25519.Verify(publicKey, checksums, signature) {
		t.Fatalf("signature envelope = %#v, want valid release-2026 signature", envelope)
	}
}

func TestSignChecksumsRejectsMissingKeyIDAndInvalidKey(t *testing.T) {
	if _, err := SignChecksums([]byte("checksums"), "", "bad"); err == nil {
		t.Fatal("SignChecksums() error = nil, want missing key ID rejection")
	}
	if _, err := SignChecksums([]byte("checksums"), "release-2026", "bad"); err == nil {
		t.Fatal("SignChecksums() error = nil, want invalid key rejection")
	}
}
