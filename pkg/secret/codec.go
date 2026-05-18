package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const ciphertextVersionPrefix = "v1:"

type Codec struct {
	gcm cipher.AEAD
}

func NewCodec(appSecret string) (*Codec, error) {
	appSecret = strings.TrimSpace(appSecret)
	if appSecret == "" {
		return nil, fmt.Errorf("app secret is required")
	}

	key := sha256.Sum256([]byte(appSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secret gcm: %w", err)
	}
	return &Codec{gcm: gcm}, nil
}

func (c *Codec) EncryptString(plaintext string) (string, error) {
	if c == nil || c.gcm == nil {
		return "", fmt.Errorf("secret codec is not configured")
	}

	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertextVersionPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Codec) DecryptString(ciphertext string) (string, error) {
	if c == nil || c.gcm == nil {
		return "", fmt.Errorf("secret codec is not configured")
	}
	if !strings.HasPrefix(ciphertext, ciphertextVersionPrefix) {
		return "", fmt.Errorf("unsupported ciphertext format")
	}

	encoded := strings.TrimPrefix(ciphertext, ciphertextVersionPrefix)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonceSize := c.gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("ciphertext is too short")
	}
	nonce := raw[:nonceSize]
	payload := raw[nonceSize:]
	plaintext, err := c.gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plaintext), nil
}

func MaskSecret(value string) string {
	if len(value) < 12 {
		return "****"
	}

	return value[:4] + "****" + value[len(value)-4:]
}
