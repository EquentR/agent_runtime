package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type SignatureEnvelope struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func VerifySignedChecksums(checksums, signature []byte, keys map[string]ed25519.PublicKey) error {
	var envelope SignatureEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(signature)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode checksum signature: %w", err)
	}
	if envelope.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported checksum signature algorithm %q", envelope.Algorithm)
	}
	publicKey, ok := keys[strings.TrimSpace(envelope.KeyID)]
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("checksum signature key %q is not trusted", envelope.KeyID)
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return fmt.Errorf("decode checksum signature: %w", err)
	}
	if !ed25519.Verify(publicKey, checksums, decoded) {
		return fmt.Errorf("checksum signature verification failed")
	}
	return nil
}

func ParseTrustedPublicKey(keyID, encoded string) (map[string]ed25519.PublicKey, error) {
	keyID = strings.TrimSpace(keyID)
	encoded = strings.TrimSpace(encoded)
	if keyID == "" || encoded == "" {
		return nil, fmt.Errorf("release signing key ID and public key are required")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode release signing public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release signing public key must be %d bytes", ed25519.PublicKeySize)
	}
	return map[string]ed25519.PublicKey{keyID: ed25519.PublicKey(raw)}, nil
}

func ParseChecksums(data []byte) (map[string]string, error) {
	checksums := make(map[string]string)
	for lineNumber, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid checksum line %d", lineNumber+1)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("invalid checksum line %d: %w", lineNumber+1, err)
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == "" || strings.ContainsAny(name, "\\/") {
			return nil, fmt.Errorf("invalid checksum filename %q", name)
		}
		if _, exists := checksums[name]; exists {
			return nil, fmt.Errorf("duplicate checksum filename %q", name)
		}
		checksums[name] = strings.ToLower(fields[0])
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("checksum file is empty")
	}
	return checksums, nil
}

func VerifyFileChecksum(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	digest := hash.Sum(nil)
	actual := hex.EncodeToString(digest[:])
	if !strings.EqualFold(strings.TrimSpace(expected), actual) {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", path, actual, expected)
	}
	return nil
}
