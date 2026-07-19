package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type signatureEnvelope struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func main() {
	checksumsPath := flag.String("checksums", "SHA256SUMS", "checksum file")
	outputPath := flag.String("output", "SHA256SUMS.sig", "signature output")
	keyID := flag.String("key-id", os.Getenv("RELEASE_SIGNING_KEY_ID"), "signing key identifier")
	privateKeyEnv := flag.String("private-key-env", "RELEASE_SIGNING_PRIVATE_KEY", "environment variable containing base64 private key")
	flag.Parse()
	checksums, err := os.ReadFile(*checksumsPath)
	if err != nil {
		fatal(err)
	}
	raw, err := SignChecksums(checksums, *keyID, os.Getenv(*privateKeyEnv))
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*outputPath, append(raw, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func SignChecksums(checksums []byte, keyID, encodedPrivateKey string) ([]byte, error) {
	if keyID == "" {
		return nil, fmt.Errorf("signing key ID is required")
	}
	privateKeyBytes, err := base64.StdEncoding.DecodeString(encodedPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode signing private key: %w", err)
	}
	var privateKey ed25519.PrivateKey
	switch len(privateKeyBytes) {
	case ed25519.SeedSize:
		privateKey = ed25519.NewKeyFromSeed(privateKeyBytes)
	case ed25519.PrivateKeySize:
		privateKey = ed25519.PrivateKey(privateKeyBytes)
	default:
		return nil, fmt.Errorf("signing private key must be %d-byte seed or %d-byte private key", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
	envelope := signatureEnvelope{KeyID: keyID, Algorithm: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))}
	return json.Marshal(envelope)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
