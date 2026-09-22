// Пакет secretbox — аутентифицированное симметричное шифрование секретов
// в покое: AES-256-GCM со случайным 12-байтовым nonce перед шифротекстом.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const KeySize = 32

var ErrInvalidKey = errors.New("secretbox: key must be 32 bytes")

var ErrCiphertextTooShort = errors.New("secretbox: ciphertext too short")

var ErrKeyNotSet = errors.New("not set")

type Box struct {
	aead cipher.AEAD

	previous []cipher.AEAD
}

func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cipher.NewGCM: %w", err)
	}
	return &Box{aead: aead}, nil
}

func NewRing(current []byte, previous [][]byte) (*Box, error) {
	box, err := New(current)
	if err != nil {
		return nil, err
	}
	for i, key := range previous {
		old, err := New(key)
		if err != nil {
			return nil, fmt.Errorf("secretbox: previous key #%d: %w", i+1, err)
		}
		box.previous = append(box.previous, old.aead)
	}
	return box, nil
}

func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretbox: read nonce: %w", err)
	}

	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (b *Box) Open(sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns+b.aead.Overhead() {
		return nil, ErrCiphertextTooShort
	}
	nonce, ciphertext := sealed[:ns], sealed[ns:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err == nil {
		return plaintext, nil
	}

	for _, old := range b.previous {
		if plaintext, oldErr := old.Open(nil, nonce, ciphertext, nil); oldErr == nil {
			return plaintext, nil
		}
	}

	return nil, err
}

func (b *Box) SealedWithCurrentKey(sealed []byte) bool {
	ns := b.aead.NonceSize()
	if len(sealed) < ns+b.aead.Overhead() {
		return false
	}
	_, err := b.aead.Open(nil, sealed[:ns], sealed[ns:], nil)
	return err == nil
}

func LoadKey(envVar string) ([]byte, error) {
	raw := os.Getenv(envVar)
	if raw == "" {
		return nil, fmt.Errorf("secretbox: %s %w", envVar, ErrKeyNotSet)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %s is not valid base64: %w", envVar, err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secretbox: %s decodes to %d bytes, expected %d", envVar, len(key), KeySize)
	}
	return key, nil
}

func ValidateKeyEnv(envVar string) error {

	_, err := FromEnv(envVar)
	if err == nil || errors.Is(err, ErrKeyNotSet) {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return nil
	}
	return err
}

func PreviousKeyEnvVar(envVar string) string { return envVar + "_PREVIOUS" }

func LoadPreviousKeys(envVar string) ([][]byte, error) {
	raw := strings.TrimSpace(os.Getenv(PreviousKeyEnvVar(envVar)))
	if raw == "" {
		return nil, nil
	}
	var keys [][]byte
	for i, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, err := base64.StdEncoding.DecodeString(entry)
		if err != nil {
			return nil, fmt.Errorf("secretbox: %s entry #%d is not valid base64: %w", PreviousKeyEnvVar(envVar), i+1, err)
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("secretbox: %s entry #%d decodes to %d bytes, expected %d", PreviousKeyEnvVar(envVar), i+1, len(key), KeySize)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func FromEnv(envVar string) (*Box, error) {
	current, err := LoadKey(envVar)
	if err != nil {
		return nil, err
	}
	previous, err := LoadPreviousKeys(envVar)
	if err != nil {
		return nil, err
	}
	return NewRing(current, previous)
}
