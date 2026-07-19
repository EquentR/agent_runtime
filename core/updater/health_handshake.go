package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type HealthHandshakeStore struct {
	path  string
	owner *FileOwner
}

func (s *HealthHandshakeStore) SetOwner(owner *FileOwner) {
	if s != nil {
		s.owner = owner
	}
}

type healthHandshake struct {
	TokenSHA256   string    `json:"token_sha256"`
	TargetVersion string    `json:"target_version"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func NewHealthHandshakeStore(updatesRoot string) (*HealthHandshakeStore, error) {
	root, err := canonicalRoot(updatesRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &HealthHandshakeStore{path: filepath.Join(root, "health-handshake.json")}, nil
}

func (s *HealthHandshakeStore) Issue(token, targetVersion string, expiresAt time.Time) error {
	if s == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(targetVersion) == "" || expiresAt.IsZero() {
		return fmt.Errorf("health handshake is incomplete")
	}
	digest := sha256.Sum256([]byte(token))
	if err := writeJSONAtomic(s.path, healthHandshake{TokenSHA256: hex.EncodeToString(digest[:]), TargetVersion: targetVersion, ExpiresAt: expiresAt.UTC()}, 0o600); err != nil {
		return err
	}
	return s.owner.apply(s.path)
}

func (s *HealthHandshakeStore) Verify(token, version string, now time.Time) error {
	if s == nil {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return fmt.Errorf("health handshake was not issued or was already consumed")
	}
	if err != nil {
		return err
	}
	var handshake healthHandshake
	if err := json.Unmarshal(raw, &handshake); err != nil {
		return err
	}
	if !now.UTC().Before(handshake.ExpiresAt) {
		return fmt.Errorf("health handshake expired")
	}
	digest := sha256.Sum256([]byte(token))
	if !strings.EqualFold(hex.EncodeToString(digest[:]), handshake.TokenSHA256) || version != handshake.TargetVersion {
		return fmt.Errorf("health handshake token or version mismatch")
	}
	return os.Remove(s.path)
}
