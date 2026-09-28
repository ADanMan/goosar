package integration

// Секретное шифрование (для БД) и подписанные redirect-билеты (для
// GitHub App install / Composio OAuth) — оба узких куска криптографии этого
// домена держатся в одном файле, потому что ни один не тянет на отдельный
// пакет: seal/unseal шифруют access_token/webhook_secret VCS и bot_token/
// app_token Slack BYO при хранении в БД (колонки *_sealed, см.
// server2/migrations/010_integrations.up.sql) тем же общим приёмом, что
// GOOSAR_MCP_SECRET_KEY в internal/agent/internal/authn: маркерный байт 0x01
// перед AES-256-GCM-шифротекстом отличает зашифрованное значение от
// сохранённого без ключа (contract требует работать и без настроенного
// ключа шифрования — тогда секрет пишется как есть, см. решения T-029 в
// decisions.md). signState/verifyState — билеты для
// `getGitHubConnectUrl`→`webhooksGithubSetupCallback` и
// `initComposioConnect`→`webhooksComposioOauthCallback`: контракт требует
// «подписанный state», не уточняя формат — поля кодируются как query-строка
// (net/url.Values), к ней приписывается дедлайн, всё подписывается
// HMAC-SHA256 и упаковывается одним base64url-блоком без JWT-подобных
// разделителей — один короткоживущий билет на один редирект, а не
// многоразовый набор клеймов.
import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"strconv"
	"time"
)

const sealedMarker = byte(0x01)

var errSealKeyMissing = errors.New("integration: ключ шифрования не настроен")

func sealKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// seal шифрует plaintext под key; key == "" — plaintext возвращается как есть,
// без маркерного байта (contract: "без ключа — сохраняется как есть").
func seal(key string, plaintext []byte) ([]byte, error) {
	if key == "" {
		return plaintext, nil
	}
	block, err := aes.NewCipher(sealKey(key))
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
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return append([]byte{sealedMarker}, ct...), nil
}

// unseal — обратная операция; пробует key, затем prevKey (ротация). ok=false
// значит запись зашифрована, но ни один из ключей не подошёл.
func unseal(key, prevKey string, sealed []byte) (plaintext []byte, ok bool) {
	if len(sealed) == 0 {
		return nil, true
	}
	if sealed[0] != sealedMarker {
		return sealed, true
	}
	ct := sealed[1:]
	for _, k := range []string{key, prevKey} {
		if k == "" {
			continue
		}
		if pt, err := openGCM(k, ct); err == nil {
			return pt, true
		}
	}
	return nil, false
}

func openGCM(key string, ct []byte) ([]byte, error) {
	block, err := aes.NewCipher(sealKey(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ct) < gcm.NonceSize() {
		return nil, errSealKeyMissing
	}
	nonce, body := ct[:gcm.NonceSize()], ct[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}

var errStateExpired = errors.New("integration: билет истёк")
var errStateInvalid = errors.New("integration: билет повреждён или подделан")

const stateDeadlineParam = "_until"

// signState кодирует fields как query-строку, дописывает дедлайн и отдаёт
// base64url(HMAC || query) — валидный до time.Now()+ttl.
func signState(secret string, fields map[string]any, ttl time.Duration) (string, error) {
	values := url.Values{}
	for k, v := range fields {
		if s, ok := v.(string); ok {
			values.Set(k, s)
		}
	}
	values.Set(stateDeadlineParam, strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))
	plain := []byte(values.Encode())
	return base64.RawURLEncoding.EncodeToString(append(stateTag(secret, plain), plain...)), nil
}

// verifyState отвергает билет с неверной подписью или истёкшим дедлайном,
// иначе возвращает его поля (без служебного stateDeadlineParam).
func verifyState(secret, token string) (map[string]any, error) {
	blob, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(blob) <= sha256.Size {
		return nil, errStateInvalid
	}
	gotTag, plain := blob[:sha256.Size], blob[sha256.Size:]
	if !hmac.Equal(gotTag, stateTag(secret, plain)) {
		return nil, errStateInvalid
	}
	values, err := url.ParseQuery(string(plain))
	if err != nil {
		return nil, errStateInvalid
	}
	deadline, err := strconv.ParseInt(values.Get(stateDeadlineParam), 10, 64)
	if err != nil || time.Now().Unix() > deadline {
		return nil, errStateExpired
	}
	values.Del(stateDeadlineParam)
	fields := make(map[string]any, len(values))
	for k := range values {
		fields[k] = values.Get(k)
	}
	return fields, nil
}

func stateTag(secret string, plain []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(plain)
	return mac.Sum(nil)
}
