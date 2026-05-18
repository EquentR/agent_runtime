package secret

import (
	"strings"
	"testing"
)

func TestSecretCodecEncryptsDecryptsAndMasks(t *testing.T) {
	codec, err := NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}

	ciphertext, err := codec.EncryptString("smtp-password")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}
	if ciphertext == "smtp-password" {
		t.Fatal("EncryptString() returned plaintext")
	}
	if !strings.HasPrefix(ciphertext, "v1:") {
		t.Fatalf("ciphertext = %q, want v1 prefix", ciphertext)
	}

	decrypted, err := codec.DecryptString(ciphertext)
	if err != nil {
		t.Fatalf("DecryptString() error = %v", err)
	}
	if decrypted != "smtp-password" {
		t.Fatalf("DecryptString() = %q, want %q", decrypted, "smtp-password")
	}

	if masked := MaskSecret("sk-abcdef"); masked != "sk-****cdef" {
		t.Fatalf("MaskSecret() = %q, want %q", masked, "sk-****cdef")
	}
	if masked := MaskSecret("short"); masked != "****" {
		t.Fatalf("MaskSecret(short) = %q, want ****", masked)
	}
}

func TestSecretCodecRejectsEmptySecret(t *testing.T) {
	if _, err := NewCodec(" "); err == nil {
		t.Fatal("NewCodec(empty) error = nil, want error")
	}
}
