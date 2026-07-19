package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EquentR/agent_runtime/core/updater"
)

type signatureEnvelope struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func main() {
	checksumsPath := flag.String("checksums", "release-assets/SHA256SUMS", "checksum file")
	signaturePath := flag.String("signature", "release-assets/SHA256SUMS.sig", "signature file")
	assetsDir := flag.String("assets-dir", "release-assets", "asset directory")
	publicKeyEnv := flag.String("public-key-env", "RELEASE_SIGNING_PUBLIC_KEY", "public key environment variable")
	flag.Parse()
	publicKey, err := base64.StdEncoding.DecodeString(os.Getenv(*publicKeyEnv))
	if err != nil {
		fatal(fmt.Errorf("decode public key: %w", err))
	}
	keyID := strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY_ID"))
	if keyID == "" || len(publicKey) != ed25519.PublicKeySize {
		fatal(fmt.Errorf("valid public key and RELEASE_SIGNING_KEY_ID are required"))
	}
	checksums, err := os.ReadFile(*checksumsPath)
	if err != nil {
		fatal(err)
	}
	signature, err := os.ReadFile(*signaturePath)
	if err != nil {
		fatal(err)
	}
	if err := VerifyRelease(checksums, signature, *assetsDir, map[string]ed25519.PublicKey{keyID: ed25519.PublicKey(publicKey)}); err != nil {
		fatal(err)
	}
}

func VerifyRelease(checksums, signature []byte, assetsDir string, keys map[string]ed25519.PublicKey) error {
	if err := updater.VerifySignedChecksums(checksums, signature, keys); err != nil {
		return err
	}
	parsed, err := updater.ParseChecksums(checksums)
	if err != nil {
		return err
	}
	for name, expected := range parsed {
		if err := updater.VerifyFileChecksum(filepath.Join(assetsDir, name), expected); err != nil {
			return err
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
