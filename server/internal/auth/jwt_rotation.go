package auth

import (
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

const jwtPreviousSecretEnvVar = "JWT_SECRET_PREVIOUS"

var (
	verifyKeyRing     [][]byte
	verifyKeyRingOnce sync.Once
)

func jwtVerificationSecrets() [][]byte {
	verifyKeyRingOnce.Do(func() {
		verifyKeyRing = append([][]byte{JWTSecret()}, retiredJWTSecrets()...)
	})
	return verifyKeyRing
}

func retiredJWTSecrets() [][]byte {
	var secrets [][]byte
	for _, entry := range strings.Split(os.Getenv(jwtPreviousSecretEnvVar), ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			secrets = append(secrets, []byte(entry))
		}
	}
	return secrets
}

func UsingPreviousJWTSecrets() bool {
	return len(jwtVerificationSecrets()) > 1
}

func ParseHS256(tokenString string) (*jwt.Token, error) {
	var lastErr error
	for _, secret := range jwtVerificationSecrets() {
		token, err := jwt.Parse(tokenString, hmacOnlyKeyFunc(secret))
		if err == nil && token.Valid {
			return token, nil
		}
		lastErr = err

		if err != nil && !isSignatureMismatchErr(err) {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = jwt.ErrTokenSignatureInvalid
	}
	return nil, lastErr
}

func hmacOnlyKeyFunc(secret []byte) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return secret, nil
	}
}

func isSignatureMismatchErr(err error) bool {
	return errors.Is(err, jwt.ErrSignatureInvalid) || errors.Is(err, jwt.ErrTokenSignatureInvalid)
}

func resetJWTSecretsForTest() {
	jwtSecretOnce = sync.Once{}
	verifyKeyRingOnce = sync.Once{}
}
