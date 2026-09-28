// MFA TOTP (RFC 6238) на crypto/hmac + запечатывание секрета через
// internal/seal (GOOSAR_MCP_SECRET_KEY — contract §1.9, "MFA": "тот же ключ
// шифрует и MCP-конфиги агентов, и TOTP-секреты"). Секрет TOTP всегда обязан
// быть зашифрован (в отличие от internal/agent, где ключ может отсутствовать
// и поле остаётся plaintext) — GOOSAR_MCP_SECRET_KEY отсутствует означает 503
// mfa_unavailable ещё на входе в handleEnrollTotp, до вызова sealSecret. До
// доводки T-029 этот файл реализовывал AES-256-GCM сам (независимо от
// internal/agent/internal/autopilot/internal/seal) — доводка сводит все
// четыре к internal/seal; формат байт-в-байт совместим с тем, что этот файл
// писал раньше (internal/seal читает и исторический формат без маркера — см.
// его пакет), так что перевод не требует миграции уже сохранённых секретов.
package authn

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // алгоритм фиксирован контрактом (MFAEnrollResponse.algorithm = "SHA1")
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/seal"
)

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	totpSkew   = 1 // ±1 шаг (±30с) на рассинхронизацию часов клиента
)

// sealSecret шифрует случайный TOTP-секрет ключом key (ErrUnavailable, если
// пуст — вызывающий код уже отверг запрос 503 раньше, сюда он попасть не
// должен).
func sealSecret(key string, plaintext []byte) ([]byte, error) {
	return seal.Seal(key, plaintext)
}

// unsealSecret — обратная операция; пробует key, затем prevKey (ротация
// GOOSAR_MCP_SECRET_KEY_PREVIOUS).
func unsealSecret(key, prevKey string, sealed_ []byte) ([]byte, error) {
	pt, ok := seal.Open(key, prevKey, sealed_)
	if !ok {
		return nil, fmt.Errorf("authn: mfa: секрет не расшифровывается ни текущим, ни предыдущим ключом")
	}
	return pt, nil
}

// --- TOTP (RFC 6238 поверх HOTP, RFC 4226) -----------------------------------

var base32Std = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret — 20 случайных байт (160 бит), обычный размер TOTP-секрета.
func newTOTPSecret() ([]byte, error) {
	return randomBytes("newTOTPSecret", 20)
}

// hotp — RFC 4226 §5.3, HMAC-SHA1, усечение до totpDigits.
func hotp(secret []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, code%mod)
}

func totpCounter(t time.Time) uint64 {
	return uint64(t.Unix()) / uint64(totpPeriod.Seconds())
}

// validateTOTP сверяет code с ±totpSkew шагами вокруг "сейчас" — обычная
// терпимость к рассинхронизации часов клиента у TOTP-реализаций. Не проверяет
// anti-replay (см. validateTOTPStep/Store.ConsumeTOTPStep) — используется там,
// где повторное предъявление того же кода не имеет значения (тестовый образец).
func validateTOTP(secret []byte, code string) bool {
	_, ok := validateTOTPStep(secret, code)
	return ok
}

// validateTOTPStep — как validateTOTP, но дополнительно возвращает номер
// HOTP-шага (RFC 4226 §5.3 counter), на котором код совпал — вызывающий код
// передаёт его в Store.ConsumeTOTPStep для защиты от повторного использования
// в пределах разрешённого окна ±1 шаг (T-029 доводка, см.
// server2/docs/decisions.md, раздел «T-029 доводка»).
func validateTOTPStep(secret []byte, code string) (step int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	now := totpCounter(time.Now())
	for skew := -totpSkew; skew <= totpSkew; skew++ {
		candidate := int64(now) + int64(skew)
		want := hotp(secret, uint64(candidate))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

// otpauthURI строит otpauth://totp/... для приложений-аутентификаторов
// (MFAEnrollResponse.otpauth_uri).
func otpauthURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{
		"secret":    {base32Std.EncodeToString(secret)},
		"issuer":    {issuer},
		"algorithm": {"SHA1"},
		"digits":    {strconv.Itoa(totpDigits)},
		"period":    {strconv.Itoa(int(totpPeriod.Seconds()))},
	}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// --- recovery codes ------------------------------------------------------------

// newRecoveryCodes генерирует n читаемых одноразовых кодов вида XXXXX-XXXXX
// (contract MFAConfirmResponse.recovery_codes: "10 one-time codes, shown
// only in this response").
func newRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		b, err := randomBytes("newRecoveryCodes", 5)
		if err != nil {
			return nil, err
		}
		raw := base32Std.EncodeToString(b) // 8 символов из {A-Z,2-7}
		codes = append(codes, raw[:4]+"-"+raw[4:])
	}
	return codes, nil
}

// normalizeRecoveryCode сводит ввод пользователя (может скопировать с
// пробелами/в нижнем регистре) к тому же виду, в котором код был захэширован
// при выпуске.
func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
