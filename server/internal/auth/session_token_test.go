package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signForTest(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(JWTSecret())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestParseSessionHS256AcceptsSessionAndRefusesTypedTokens(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-session-token")
	resetJWTSecretsForTest()
	t.Cleanup(resetJWTSecretsForTest)

	exp := jwt.NewNumericDate(time.Now().Add(5 * time.Minute))

	session := signForTest(t, jwt.MapClaims{"sub": "11111111-1111-1111-1111-111111111111", "exp": exp})
	if _, err := ParseSessionHS256(session); err != nil {
		t.Fatalf("session token must be accepted, got %v", err)
	}

	ticket := signForTest(t, jwt.MapClaims{"sub": "11111111-1111-1111-1111-111111111111", "typ": "mfa", "exp": exp})
	if _, err := ParseSessionHS256(ticket); !errors.Is(err, ErrNotSessionToken) {
		t.Fatalf("mfa ticket must be refused as a session, got %v", err)
	}

	state := signForTest(t, jwt.MapClaims{"typ": "oidc_state", "exp": exp})
	if _, err := ParseSessionHS256(state); !errors.Is(err, ErrNotSessionToken) {
		t.Fatalf("oidc_state must be refused as a session, got %v", err)
	}
}
