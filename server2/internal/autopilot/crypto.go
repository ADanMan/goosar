// crypto.go — запечатывание секрета подписи вебхука (strig_signing_secret_sealed,
// contract: setAutopilotTriggerSigningSecret). Ни один существующий пакет
// этой сессии ещё не реализует шифрование GOOSAR_MCP_SECRET_KEY (см.
// server2/docs/decisions.md, T-026 пункт 2 — TOTP-секреты тоже оставлены
// незашифрованными/501 по той же причине), так что готового хелпера нет;
// этот файл — маленький самостоятельный AES-256-GCM поверх того же ключа
// (§1.9: "тот же ключ шифрует и MCP-конфиги агентов, и TOTP-секреты" — здесь
// то же самое расширено на секрет подписи вебхука автопилота, тот же дух
// решения). См. server2/docs/decisions.md, раздел T-028, пункт про
// шифрование.
package autopilot

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrEncryptionUnavailable — GOOSAR_MCP_SECRET_KEY не задан: сохранить
// секрет подписи невозможно (contract: тот же 503, что и MFA/секреты
// воркспейса, см. docs/50-api-contract.md §1.9 "MFA").
var ErrEncryptionUnavailable = errors.New("autopilot: GOOSAR_MCP_SECRET_KEY не настроен")

func sealSecret(key, plaintext string) ([]byte, error) {
	if key == "" {
		return nil, ErrEncryptionUnavailable
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, fmt.Errorf("autopilot: инициализация шифра: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("autopilot: инициализация GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("autopilot: генерация nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func openSecret(key string, sealed []byte) (string, error) {
	if key == "" {
		return "", ErrEncryptionUnavailable
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", fmt.Errorf("autopilot: инициализация шифра: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("autopilot: инициализация GCM: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("autopilot: повреждённый запечатанный секрет")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("autopilot: расшифровка секрета: %w", err)
	}
	return string(plaintext), nil
}

// deriveKey сводит произвольной длины GOOSAR_MCP_SECRET_KEY к ровно 32
// байтам, которых требует AES-256.
func deriveKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}
