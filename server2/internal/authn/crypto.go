package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"math/big"
)

// digest — необратимый отпечаток секрета (код входа, PAT, секрет сессии),
// который хранится в БД вместо самого секрета. sha256 достаточен здесь: все
// секреты уже высокоэнтропийны (генерируются randomToken/randomCode), это не
// хеширование пароля низкой энтропии, где нужен bcrypt/argon2.
func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// constantTimeEqual — сравнение отпечатков без утечки по времени.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// randomToken генерирует криптостойкий токен фиксированной длины в base32
// (без паддинга, только заглавные буквы/цифры — удобно копировать вручную).
func randomToken(prefix string, nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("authn: генерация токена: %w", err)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return prefix + enc, nil
}

// randomCode генерирует numDigits-значный числовой код (по умолчанию для
// email-кода входа — 6 цифр), с ведущими нулями.
func randomCode(numDigits int) (string, error) {
	max := int64(1)
	for i := 0; i < numDigits; i++ {
		max *= 10
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return "", fmt.Errorf("authn: генерация кода: %w", err)
	}
	return fmt.Sprintf("%0*d", numDigits, n.Int64()), nil
}
