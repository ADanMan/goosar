// Пакет totp реализует одноразовые пароли по времени (RFC 6238) для второго
// фактора. Параметры зафиксированы: HMAC-SHA-1, шаг 30 секунд, 6 цифр — именно это
// реально поддерживают приложения-аутентификаторы.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	Step = 30 * time.Second

	Digits = 6

	SecretBytes = 20

	Skew = 1
)

var base32Codec = base32.StdEncoding.WithPadding(base32.NoPadding)

func GenerateSecret() ([]byte, error) {
	секрет := make([]byte, SecretBytes)
	if _, err := rand.Read(секрет); err != nil {
		return nil, fmt.Errorf("totp: read random: %w", err)
	}
	return секрет, nil
}

func EncodeSecret(секрет []byte) string { return base32Codec.EncodeToString(секрет) }

func DecodeSecret(s string) ([]byte, error) {
	stripped := strings.NewReplacer(" ", "", "-", "", "=", "").Replace(s)
	return base32Codec.DecodeString(strings.ToUpper(stripped))
}

func Step64(t time.Time) int64 { return t.Unix() / int64(Step/time.Second) }

func Code(секрет []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))

	mac := hmac.New(sha1.New, секрет)
	mac.Write(counter[:])
	digest := mac.Sum(nil)

	truncated := dynamicTruncate(digest)
	return fmt.Sprintf("%0*d", Digits, truncated%pow10(Digits))
}

func dynamicTruncate(digest []byte) uint32 {
	offset := digest[len(digest)-1] & 0x0f
	return binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
}

func pow10(n int) uint32 {
	out := uint32(1)
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}

func Validate(секрет []byte, code string, at time.Time) (int64, bool) {
	trimmed := strings.TrimSpace(code)
	if len(trimmed) != Digits {
		return 0, false
	}

	current := Step64(at)
	for delta := -Skew; delta <= Skew; delta++ {
		step := current + int64(delta)
		if subtle.ConstantTimeCompare([]byte(Code(секрет, step)), []byte(trimmed)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func URI(issuer, account string, секрет []byte) string {
	label := url.PathEscape(issuer + ":" + account)

	params := url.Values{}
	params.Set("secret", EncodeSecret(секрет))
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", Digits))
	params.Set("period", fmt.Sprintf("%d", int(Step/time.Second)))

	return "otpauth://totp/" + label + "?" + params.Encode()
}
