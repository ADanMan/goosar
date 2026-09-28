// Package seal — единственный общий примитив запечатывания секретных значений
// ключом GOOSAR_MCP_SECRET_KEY (docs/50-api-contract.md §1.9, "MFA": "тот же
// ключ шифрует и MCP-конфиги агентов, и TOTP-секреты"). До доводки T-029 три
// независимые реализации одного и того же AES-256-GCM поверх sha256(key) жили
// в internal/agent (crypto.go), internal/autopilot (crypto.go) и здесь —
// формат отличался в деталях (маркерный байт у agent, его отсутствие у
// autopilot/seal); эта доводка сводит все три к одному формату и одному
// пакету, оставляя обе стороны совместимости (см. Open/OpenOptional): новые
// значения всегда пишутся в едином формате, старые — читаются в обоих
// исторических форматах, без миграции данных.
//
// Формат (Seal/SealOptional, "зашифровано"): один байт-маркер 0x01, затем
// AES-256-GCM (nonce фиксированной длины GCM, затем шифротекст+тег). Ключ
// AES — sha256(GOOSAR_MCP_SECRET_KEY).
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

// sealedMarker — первый байт запечатанного значения в текущем формате.
// Валидный JSON (используемый SealOptional-колонками без ключа) никогда не
// начинается с этого байта, поэтому OpenOptional различает "зашифровано" и
// "как есть" однозначно, без отдельной колонки-флага.
const sealedMarker = byte(0x01)

// ErrUnavailable — ключ шифрования не настроен на сервере (GOOSAR_MCP_SECRET_KEY
// пуст). Контракт требует в этом случае 503 у операций, которые обязаны
// писать секрет зашифрованным (Seal); для полей, допускающих запись без
// ключа как есть, вызывающий код использует SealOptional, а не Seal.
var ErrUnavailable = errors.New("seal: GOOSAR_MCP_SECRET_KEY не настроен")

// Available сообщает, настроен ли ключ шифрования на сервере.
func Available(key string) bool { return key != "" }

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func gcmFor(key string) (cipher.AEAD, error) {
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal шифрует plaintext ключом key (маркер + AES-256-GCM, случайный nonce
// впереди шифротекста). Возвращает ErrUnavailable, если key пуст — для полей,
// у которых контракт не допускает запись без ключа (TOTP-секреты, секреты
// подписи вебхуков автопилотов, MCP-серверы/config воркспейса и деплоя).
func Seal(key string, plaintext []byte) ([]byte, error) {
	if key == "" {
		return nil, ErrUnavailable
	}
	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return append([]byte{sealedMarker}, ct...), nil
}

// SealOptional — как Seal, но пустой key не ошибка: значение возвращается как
// есть, без шифрования (encrypted=false) — для полей, контракт которых прямо
// разрешает запись plaintext без настроенного ключа (agent op_runtime_config_sealed/
// op_custom_env_sealed/op_mcp_config_sealed: "без ключа — сохраняется как есть").
func SealOptional(key string, plaintext []byte) (sealed []byte, encrypted bool, err error) {
	if key == "" {
		return plaintext, false, nil
	}
	sealed, err = Seal(key, plaintext)
	return sealed, err == nil, err
}

func openGCM(key string, ciphertext []byte) ([]byte, error) {
	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("seal: шифротекст короче nonce")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// tryKeys пробует key, затем prevKey (ротация GOOSAR_MCP_SECRET_KEY_PREVIOUS).
func tryKeys(key, prevKey string, ciphertext []byte) (plaintext []byte, ok bool) {
	for _, k := range []string{key, prevKey} {
		if k == "" {
			continue
		}
		if pt, err := openGCM(k, ciphertext); err == nil {
			return pt, true
		}
	}
	return nil, false
}

// Open расшифровывает sealed, пробуя сперва key, затем prevKey. Понимает два
// формата байт-в-байт: текущий (маркер+GCM, см. Seal) и исторический
// (голый GCM без маркера — старые internal/autopilot/internal/seal/internal/authn
// значения, записанные до этой доводки) — сперва пробует маркерный формат,
// затем, если не подошло, весь sealed целиком как nonce+шифротекст без
// маркера. GCM-тег аутентификации делает эту разницу безопасной: подобрать
// оба варианта одновременно случайно нельзя. ok=false — ни один формат/ключ
// не подошёл.
func Open(key, prevKey string, sealed []byte) (plaintext []byte, ok bool) {
	if len(sealed) == 0 {
		return nil, true
	}
	if sealed[0] == sealedMarker {
		if pt, ok := tryKeys(key, prevKey, sealed[1:]); ok {
			return pt, true
		}
	}
	// Исторический формат без маркера (autopilot/seal до унификации).
	return tryKeys(key, prevKey, sealed)
}

// OpenOptional — обратная операция к SealOptional: если sealed не начинается
// с маркера, значение считается незашифрованным plaintext (as-is), как и
// писал SealOptional при пустом ключе. encrypted сообщает, было ли значение
// действительно зашифровано (для восстановления op_mcp_config_encrypted и
// подобных полей ответа). ok=false — значение помечено как зашифрованное
// (маркер есть), но ни один ключ не подошёл.
func OpenOptional(key, prevKey string, sealed []byte) (plaintext []byte, encrypted bool, ok bool) {
	if len(sealed) == 0 {
		return nil, false, true
	}
	if sealed[0] != sealedMarker {
		return sealed, false, true
	}
	pt, ok := tryKeys(key, prevKey, sealed[1:])
	return pt, true, ok
}

// SealJSON сериализует v в JSON и запечатывает результат (Seal — ключ обязателен).
func SealJSON(key string, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Seal(key, raw)
}

// OpenJSON расшифровывает sealed (формат Open) и разбирает JSON в out.
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

// SealJSONOptional — SealOptional поверх JSON-сериализации v.
func SealJSONOptional(key string, v any) (sealed []byte, encrypted bool, err error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false, err
	}
	return SealOptional(key, raw)
}

// OpenJSONOptional — OpenOptional поверх JSON: разбирает результат в out.
// ok=false — то же самое, что и OpenOptional (зашифровано, но ключ не
// подошёл); JSON, который не разбирается, тоже даёт ok=false.
func OpenJSONOptional(key, prevKey string, sealed []byte, out any) (encrypted bool, ok bool) {
	if len(sealed) == 0 {
		return false, true
	}
	raw, encrypted, ok := OpenOptional(key, prevKey, sealed)
	if !ok {
		return encrypted, false
	}
	return encrypted, json.Unmarshal(raw, out) == nil
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
