package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func testCipher(t *testing.T) *secretCipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	cipher, err := newSecretCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestSecretCipherBindsCiphertextToRecordID(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := cipher.seal("k_one", "sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "sk-secret-value") {
		t.Fatal("ciphertext contains plaintext")
	}
	plain, err := cipher.open("k_one", encrypted)
	if err != nil || plain != "sk-secret-value" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	if _, err := cipher.open("k_two", encrypted); err == nil {
		t.Fatal("ciphertext decrypted under a different record ID")
	}
}

func TestSecretCipherRejectsShortMasterKey(t *testing.T) {
	if _, err := newSecretCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short master key was accepted")
	}
}

func TestMaskSecretNeverReturnsTheWholeValue(t *testing.T) {
	for _, secret := range []string{"short", "12345678", "sk-1234567890abcdef"} {
		masked := maskSecret(secret)
		if masked == secret || strings.Contains(masked, secret) {
			t.Fatalf("maskSecret(%q) = %q", secret, masked)
		}
	}
}
