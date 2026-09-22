package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
)

const defaultJWTSecret = "goosar-dev-secret-change-in-production"

var weakJWTSecrets = map[string]struct{}{
	defaultJWTSecret:          {},
	"change-me-in-production": {},
}

func isWeakJWTSecret(secret string) bool {
	_, weak := weakJWTSecrets[secret]
	return weak
}

var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

func JWTSecret() []byte {
	jwtSecretOnce.Do(func() {

		if secret := strings.TrimSpace(os.Getenv("JWT_SECRET")); secret != "" {
			jwtSecret = []byte(secret)
		} else {
			jwtSecret = []byte(defaultJWTSecret)
		}
	})
	return jwtSecret
}

func ValidateJWTSecret() error {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return nil
	}

	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	switch {
	case secret == "":
		return fmt.Errorf("JWT_SECRET is not set and APP_ENV=production — refusing to start with the built-in development signing secret. Set JWT_SECRET to a strong random value (e.g. `openssl rand -hex 32`)")
	case isWeakJWTSecret(secret):
		return fmt.Errorf("JWT_SECRET is set to a known placeholder value and APP_ENV=production — refusing to start. Set JWT_SECRET to a strong random value (e.g. `openssl rand -hex 32`)")
	}

	for _, entry := range strings.Split(os.Getenv(jwtPreviousSecretEnvVar), ",") {
		if isWeakJWTSecret(strings.TrimSpace(entry)) {
			return fmt.Errorf("%s lists a known placeholder signing secret and APP_ENV=production — refusing to start: tokens forged with it would still verify. Remove that entry (sessions signed with it end, which is the point)", jwtPreviousSecretEnvVar)
		}
	}
	return nil
}

func UsingDevJWTSecret() bool {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	return secret == "" || isWeakJWTSecret(secret)
}

func randomTokenHex(prefix, what string) (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate %s: %w", what, err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func GeneratePATToken() (string, error) {
	return randomTokenHex(PATPrefix, "PAT token")
}

func GenerateDaemonToken() (string, error) {
	return randomTokenHex("mdt_", "daemon token")
}

func GenerateAgentTaskToken() (string, error) {
	return randomTokenHex("mat_", "agent task token")
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
