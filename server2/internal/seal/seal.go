// Package seal — общий примитив запечатывания секретных значений ключом
// GOOSAR_MCP_SECRET_KEY (docs/50-api-contract.md §1.9, "MFA": "тот же ключ
// шифрует и MCP-конфиги агентов, и TOTP-секреты"). До этой сессии (T-029) две
// независимые реализации одного и того же AES-256-GCM поверх sha256(key) уже
// жили в internal/agent (crypto.go) и internal/autopilot (crypto.go) — они не
// трогаются здесь (см. server2/docs/decisions.md, раздел T-029, за
// обоснование), чтобы не рисковать чужими файлами во время параллельной
// работы; этот пакет — аддитивная точка, которой пользуется только
// internal/deployment (config-слои воркспейса, MCP-серверы деплоя/
// воркспейса, персональные credential-значения). Будущая уборка может
// перевести agent/autopilot на него же — не обязательно для T-029.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// ErrUnavailable — ключ шифрования не настроен на сервере (GOOSAR_MCP_SECRET_KEY
// пуст). Контракт требует в этом случае 503 у любой операции, которая писала
// бы секрет (в отличие от internal/agent, где отсутствие ключа означает
// запись plaintext — здесь такой возможности контракт не даёт, см.
// docs/50-api-contract.md §1.9 "Валидация (workspace-mcp-servers)" и другие
// разделы про MCP-серверы/конфиги: "Требует GOOSAR_MCP_SECRET_KEY — иначе 503").
var ErrUnavailable = errors.New("seal: GOOSAR_MCP_SECRET_KEY не настроен")

// Available сообщает, настроен ли ключ шифрования на сервере.
func Available(key string) bool { return key != "" }

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// Seal шифрует plaintext ключом key (AES-256-GCM, случайный nonce впереди
// шифротекста). Возвращает ErrUnavailable, если key пуст.
func Seal(key string, plaintext []byte) ([]byte, error) {
	if key == "" {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open расшифровывает ciphertext, пробуя сперва key, затем prevKey (ротация
// ключа, GOOSAR_MCP_SECRET_KEY_PREVIOUS). ok=false — ни один ключ не подошёл.
func Open(key, prevKey string, ciphertext []byte) (plaintext []byte, ok bool) {
	if len(ciphertext) == 0 {
		return nil, true
	}
	for _, k := range []string{key, prevKey} {
		if k == "" {
			continue
		}
		if pt, err := open(k, ciphertext); err == nil {
			return pt, true
		}
	}
	return nil, false
}

func open(key string, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("seal: шифротекст короче nonce")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// SealJSON сериализует v в JSON и запечатывает результат.
func SealJSON(key string, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Seal(key, raw)
}

// OpenJSON расшифровывает sealed и разбирает JSON в out.
func OpenJSON(key, prevKey string, sealed []byte, out any) (ok bool) {
	if len(sealed) == 0 {
		return true
	}
	raw, ok := Open(key, prevKey, sealed)
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// HashHex — sha256 отпечаток произвольных байт в hex, для admin-аудита
// "хэш до/после" (контракт нигде не отдаёт содержимое секретов/документов в
// аудит, только хэш — docs/50-api-contract.md §7 "побочные эффекты").
func HashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashJSON сериализует v и возвращает HashHex её байт; ошибка сериализации
// даёт пустую строку (аудит-хэш — диагностика, не должен ронять запрос).
func HashJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return HashHex(raw)
}
