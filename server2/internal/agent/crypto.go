package agent

// Шифрование секретных JSON-полей агента (runtime_config/mcp_config/custom_env,
// колонки op_*_sealed в 003_agents.up.sql). Контракт (docs/50-api-contract.md
// §1.9, "MFA") прямо называет механизм: "GOOSAR_MCP_SECRET_KEY (тот же ключ
// шифрует и MCP-конфиги агентов, и TOTP-секреты; без него — ... сохранённые
// MCP-конфиги пишутся в БД plaintext), GOOSAR_MCP_SECRET_KEY_PREVIOUS (ротация
// ключа)". Решение этой сессии (см. server2/docs/decisions.md, раздел T-028):
// то же самое правило распространяется единообразно на все три sealed-поля
// агента (не только mcp_config, для которого контракт заводит отдельную
// колонку-флаг op_mcp_config_encrypted) — раз колонка называется одинаково
// (*_sealed) и данные-модель нигде не описывает разное поведение для них.
// Поскольку op_runtime_config_sealed/op_custom_env_sealed не имеют парной
// колонки "зашифровано ли", состояние кодируется самим содержимым: один байт
// маркера перед AES-256-GCM-шифротекстом (0x01), либо его отсутствие —
// значит запись сделана без настроенного ключа и лежит как обычный JSON.
// Валидный JSON никогда не начинается с байта 0x01, так что различение
// однозначно и не требует новой миграции.
import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
)

const sealMarkerEncrypted = byte(0x01)

var errSealKeyEmpty = errors.New("agent: ключ шифрования пуст")

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func encryptGCM(secret string, plaintext []byte) ([]byte, error) {
	if secret == "" {
		return nil, errSealKeyEmpty
	}
	block, err := aes.NewCipher(deriveKey(secret))
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

func decryptGCM(secret string, ciphertext []byte) ([]byte, error) {
	if secret == "" {
		return nil, errSealKeyEmpty
	}
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("agent: шифротекст короче nonce")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// sealJSON сериализует v и, если key задан, шифрует результат (с маркером
// sealMarkerEncrypted первым байтом); при пустом key возвращает JSON как
// есть (contract: "без ключа — plaintext"). encrypted сообщает, что
// фактически произошло — для op_mcp_config_encrypted и подобных полей ответа.
func sealJSON(key string, v any) (sealed []byte, encrypted bool, err error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false, err
	}
	if key == "" {
		return raw, false, nil
	}
	ct, err := encryptGCM(key, raw)
	if err != nil {
		return nil, false, err
	}
	return append([]byte{sealMarkerEncrypted}, ct...), true, nil
}

// openJSON — обратная операция: расшифровывает sealed (пробуя key, затем
// prevKey при ротации, contract GOOSAR_MCP_SECRET_KEY_PREVIOUS) и разбирает
// JSON в out. ok=false — значение зашифровано, но ни один из ключей не
// подошёл (mcp_config_redacted у вызывающего кода).
func openJSON(key, prevKey string, sealed []byte, out any) (ok bool) {
	if len(sealed) == 0 {
		return true
	}
	if sealed[0] != sealMarkerEncrypted {
		return json.Unmarshal(sealed, out) == nil
	}
	ct := sealed[1:]
	if key != "" {
		if pt, err := decryptGCM(key, ct); err == nil {
			return json.Unmarshal(pt, out) == nil
		}
	}
	if prevKey != "" {
		if pt, err := decryptGCM(prevKey, ct); err == nil {
			return json.Unmarshal(pt, out) == nil
		}
	}
	return false
}

// wasSealed — sealed кодирует зашифрованное значение (для восстановления
// op_mcp_config_encrypted у уже прочитанных строк без повторного decrypt).
func wasSealed(sealed []byte) bool {
	return len(sealed) > 0 && sealed[0] == sealMarkerEncrypted
}
