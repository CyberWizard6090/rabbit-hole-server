package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const webhookSecretPrefix = "whsec_"

var errWebhookSecretUnreadable = errors.New("webhook secret cannot be decrypted")

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(masterSecret string) (*SecretBox, error) {
	if masterSecret == "" {
		return nil, errors.New("secret box needs a master secret")
	}
	mac := hmac.New(sha256.New, []byte(masterSecret))
	_, _ = mac.Write([]byte("rabbit-hole-server/webhook-secret-encryption/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create webhook secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create webhook secret cipher: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) additionalData(channelID uint) []byte {
	return []byte(fmt.Sprintf("notification-channel:%d", channelID))
}

func (b *SecretBox) Seal(channelID uint, plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read random nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), b.additionalData(channelID))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Open(channelID uint, encoded string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errWebhookSecretUnreadable
	}
	nonce, body := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, body, b.additionalData(channelID))
	if err != nil {
		return "", errWebhookSecretUnreadable
	}
	return string(plain), nil
}

func newWebhookSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random secret: %w", err)
	}
	return webhookSecretPrefix + hex.EncodeToString(raw), nil
}

func newDeliveryID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random delivery id: %w", err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	h := hex.EncodeToString(raw)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
