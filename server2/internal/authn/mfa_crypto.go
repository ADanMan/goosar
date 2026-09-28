// MFA TOTP (RFC 6238) на crypto/hmac + запечатывание секрета AES-256-GCM на
// GOOSAR_MCP_SECRET_KEY (contract §1.9, "MFA": "тот же ключ шифрует и
// MCP-конфиги агентов, и TOTP-секреты"). Секрет TOTP всегда обязан быть
// зашифрован (в отличие от internal/agent, где ключ может отсутствовать и
// поле остаётся plaintext) — GOOSAR_MCP_SECRET_KEY отсутствует означает
// 503 mfa_unavailable ещё на входе в handleEnrollTotp, до вызова sealSecret.
package authn

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // алгоритм фиксирован контрактом (MFAEnrollResponse.algorithm = "SHA1")
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	totpSkew   = 1 // ±1 шаг (±30с) на рассинхронизацию часов клиента
)

// sealSecret шифрует случайный TOTP-секрет AES-256-GCM на key (nonce впереди
// шифротекста). key пуст — вызывающий код уже отверг запрос 503 раньше, сюда
// он попасть не должен (errSealKeyEmpty на этот случай всё равно есть).
func sealSecret(key string, plaintext []byte) ([]byte, error) {
	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("authn: mfa: генерация nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// unsealSecret — обратная операция; пробует key, затем prevKey (ротация
// GOOSAR_MCP_SECRET_KEY_PREVIOUS), тем же порядком, что internal/agent.
func unsealSecret(key, prevKey string, sealed []byte) ([]byte, error) {
	if gcm, err := gcmFor(key); err == nil {
		if pt, err := openGCM(gcm, sealed); err == nil {
			return pt, nil
		}
	}
	if prevKey != "" {
		if gcm, err := gcmFor(prevKey); err == nil {
			if pt, err := openGCM(gcm, sealed); err == nil {
				return pt, nil
			}
		}
	}
	return nil, errors.New("authn: mfa: секрет не расшифровывается ни текущим, ни предыдущим ключом")
}

func gcmFor(key string) (cipher.AEAD, error) {
	if key == "" {
		return nil, errors.New("authn: mfa: ключ шифрования пуст")
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func openGCM(gcm cipher.AEAD, sealed []byte) ([]byte, error) {
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("authn: mfa: шифротекст короче nonce")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
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
// терпимость к рассинхронизации часов клиента у TOTP-реализаций.
func validateTOTP(secret []byte, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	now := totpCounter(time.Now())
	for skew := -totpSkew; skew <= totpSkew; skew++ {
		want := hotp(secret, uint64(int64(now)+int64(skew)))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
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
