package updater

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrOperationAuthorization = errors.New("operation authorization failed")

type AuthorizationBinding struct {
	UserID    uint64
	SessionID string
	Action    string
	Target    string
}

type authorizationGrant struct {
	binding   AuthorizationBinding
	expiresAt time.Time
}

type OperationAuthorizer struct {
	mu     sync.Mutex
	grants map[[sha256.Size]byte]authorizationGrant
	now    func() time.Time
}

func NewOperationAuthorizer(now func() time.Time) *OperationAuthorizer {
	if now == nil {
		now = time.Now
	}
	return &OperationAuthorizer{grants: make(map[[sha256.Size]byte]authorizationGrant), now: now}
}

func (a *OperationAuthorizer) Issue(binding AuthorizationBinding, ttl time.Duration) (string, time.Time, error) {
	if a == nil || binding.UserID == 0 || binding.SessionID == "" || binding.Action == "" || binding.Target == "" {
		return "", time.Time{}, fmt.Errorf("authorization binding is incomplete")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	expiresAt := a.now().UTC().Add(ttl)
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, grant := range a.grants {
		if !a.now().UTC().Before(grant.expiresAt) {
			delete(a.grants, key)
		}
	}
	a.grants[digest] = authorizationGrant{binding: binding, expiresAt: expiresAt}
	return token, expiresAt, nil
}

func (a *OperationAuthorizer) Consume(token string, binding AuthorizationBinding) error {
	if a == nil || token == "" {
		return fmt.Errorf("%w: authorization is required", ErrOperationAuthorization)
	}
	digest := sha256.Sum256([]byte(token))
	a.mu.Lock()
	defer a.mu.Unlock()
	grant, ok := a.grants[digest]
	if !ok {
		return fmt.Errorf("%w: authorization is invalid or already used", ErrOperationAuthorization)
	}
	if !a.now().UTC().Before(grant.expiresAt) {
		delete(a.grants, digest)
		return fmt.Errorf("%w: authorization expired", ErrOperationAuthorization)
	}
	if grant.binding != binding {
		return fmt.Errorf("%w: authorization binding mismatch", ErrOperationAuthorization)
	}
	delete(a.grants, digest)
	return nil
}
