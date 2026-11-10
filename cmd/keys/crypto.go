package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

type secretCipher struct {
	aead cipher.AEAD
}

func newSecretCipher(encoded string) (*secretCipher, error) {
	key, err := decodeKey(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize AES-GCM: %w", err)
	}
	return &secretCipher{aead: aead}, nil
}

func decodeKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("encryption key is empty")
	}
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, decoder := range decoders {
		key, err := decoder.DecodeString(encoded)
		if err == nil {
			if len(key) != 32 {
				return nil, fmt.Errorf("encryption key must decode to 32 bytes, got %d", len(key))
			}
			return key, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("decode encryption key: %w", lastErr)
}

func (c *secretCipher) seal(id, plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("secret is empty")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate encryption nonce: %w", err)
	}
	out := c.aead.Seal(nil, nonce, []byte(plaintext), secretAAD(id))
	payload := append(nonce, out...)
	return "v1:" + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (c *secretCipher) open(id, encoded string) (string, error) {
	if !strings.HasPrefix(encoded, "v1:") {
		return "", errors.New("unsupported encrypted secret version")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", errors.New("encrypted secret is truncated")
	}
	plain, err := c.aead.Open(nil, payload[:nonceSize], payload[nonceSize:], secretAAD(id))
	if err != nil {
		return "", errors.New("decrypt secret failed")
	}
	return string(plain), nil
}

func secretAAD(id string) []byte {
	return []byte("go-sites/keys/secret/v1/" + id)
}

func maskSecret(secret string) string {
	runes := []rune(secret)
	switch {
	case len(runes) >= 16:
		return string(runes[:4]) + "••••••••" + string(runes[len(runes)-4:])
	case len(runes) >= 8:
		return string(runes[:2]) + "••••••" + string(runes[len(runes)-2:])
	default:
		return "••••••••"
	}
}
