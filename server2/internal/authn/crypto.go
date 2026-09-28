package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// tokenKind различает форму значения из Authorization: Bearer / cookie
// goosar_auth по префиксу — components/securitySchemes/bearerAuth в
// контракте перечисляет их явно (gsl_, gsln_, mdt_, mat_, иначе — сессионный
// JWT). Вынесено сюда, а не оставлено россыпью strings.HasPrefix по
// middleware.go, чтобы список префиксов жил в одном месте, рядом с digest/
// randomToken, которые их и производят.
type tokenKind int

const (
	kindSessionJWT tokenKind = iota
	kindPAT
	kindCloudPAT
	kindDaemonToken
	kindTaskToken
)

var tokenPrefixes = []struct {
	prefix string
	kind   tokenKind
}{
	{patPrefix, kindPAT},
	{"gsln_", kindCloudPAT},
	{"mdt_", kindDaemonToken},
	{"mat_", kindTaskToken},
}

// classifyToken сообщает вид токена по его префиксу; отсутствие совпадения
// означает "сессионный JWT" — единственная форма без собственного префикса.
func classifyToken(token string) tokenKind {
	for _, p := range tokenPrefixes {
		if strings.HasPrefix(token, p.prefix) {
			return p.kind
		}
	}
	return kindSessionJWT
}

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// digest — необратимый отпечаток секрета (код входа, PAT, секрет сессии),
// который хранится в БД вместо самого секрета. sha256 достаточен здесь: все
// секреты уже высокоэнтропийны (генерируются randomToken/randomCode), это не
// хеширование пароля низкой энтропии, где нужен bcrypt/argon2.
func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// constantTimeEqual сравнивает два отпечатка без утечки по времени выполнения.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// randomBytes читает n криптостойких случайных байт или возвращает
// обёрнутую ошибку rand.Read с контекстом caller — общий низ для
// randomToken и randomCode, оба зависят только от источника энтропии, не от
// формата результата.
func randomBytes(caller string, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("authn: %s: чтение случайных байт: %w", caller, err)
	}
	return b, nil
}

// randomToken генерирует токен из nBytes случайных байт в base32 без
// паддинга (удобно копировать вручную) с префиксом, обозначающим тип
// секрета (gsl_, sess_, ...).
func randomToken(prefix string, nBytes int) (string, error) {
	b, err := randomBytes("randomToken", nBytes)
	if err != nil {
		return "", err
	}
	return prefix + base32NoPad.EncodeToString(b), nil
}

// randomCode генерирует numDigits-значный числовой код (для email-кода
// входа — 6 цифр) с сохранением ведущих нулей.
func randomCode(numDigits int) (string, error) {
	upperBound := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(numDigits)), nil)
	n, err := rand.Int(rand.Reader, upperBound)
	if err != nil {
		return "", fmt.Errorf("authn: randomCode: генерация: %w", err)
	}
	return fmt.Sprintf("%0*d", numDigits, n.Int64()), nil
}

// signHMACPayload/verifyHMACPayload — короткоживущий подписанный опаковый
// токен для cookie, которым не нужна отдельная строка в БД (например
// OIDC state/nonce, oidc.go): payload в base64url, "." и HMAC-SHA256 подпись
// в base64url поверх него. Тот же приём, что MintDaemonToken/verifyDaemonToken
// в daemon_token.go, вынесенный сюда как общий примитив для payload
// произвольного вида (не только "workspaceID|daemonID").
func signHMACPayload(secret, payload string) string {
	sig := hmac.New(sha256.New, []byte(secret))
	sig.Write([]byte(payload))
	mac := base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + mac
}

func verifyHMACPayload(secret, token string) (payload string, ok bool) {
	encPayload, mac, found := strings.Cut(token, ".")
	if !found {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encPayload)
	if err != nil {
		return "", false
	}
	sig := hmac.New(sha256.New, []byte(secret))
	sig.Write(raw)
	want := base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(mac)) != 1 {
		return "", false
	}
	return string(raw), true
}
