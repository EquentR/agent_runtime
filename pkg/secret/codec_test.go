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

	if masked := MaskSecret("sk-abcdef"); masked != "****" {
		t.Fatalf("MaskSecret(medium) = %q, want ****", masked)
	}
	if masked := MaskSecret("abcdefghijkl"); masked != "abcd****ijkl" {
		t.Fatalf("MaskSecret(long) = %q, want %q", masked, "abcd****ijkl")
	}
	if masked := MaskSecret("short"); masked != "****" {
		t.Fatalf("MaskSecret(short) = %q, want ****", masked)
	}
}

func TestSecretCodecEncryptsSamePlaintextWithDifferentCiphertext(t *testing.T) {
	codec, err := NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}

	first, err := codec.EncryptString("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptString(first) error = %v", err)
	}
	second, err := codec.EncryptString("same-plaintext")
	if err != nil {
		t.Fatalf("EncryptString(second) error = %v", err)
	}
	if first == second {
		t.Fatal("EncryptString() returned identical ciphertexts for same plaintext")
	}
}

func TestSecretCodecRejectsTamperedCiphertext(t *testing.T) {
	codec, err := NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}

	ciphertext, err := codec.EncryptString("smtp-password")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}
	tampered := ciphertext[:len(ciphertext)-1] + "A"
	if _, err := codec.DecryptString(tampered); err == nil {
		t.Fatal("DecryptString(tampered) error = nil, want error")
	}
}

func TestSecretCodecRejectsEmptySecret(t *testing.T) {
	if _, err := NewCodec(" "); err == nil {
		t.Fatal("NewCodec(empty) error = nil, want error")
	}
}
