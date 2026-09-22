package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signWith(t *testing.T, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestParseHS256AcceptsPreviousSecret(t *testing.T) {
	old := signWith(t, "old-secret-value")
	t.Setenv("JWT_SECRET", "new-secret-value")
	t.Setenv("JWT_SECRET_PREVIOUS", "old-secret-value")
	resetJWTSecretsForTest()

	token, err := ParseHS256(old)
	if err != nil || !token.Valid {
		t.Fatalf("token signed with the retired secret was rejected: %v", err)
	}
}

func TestParseHS256RejectsUnknownSecret(t *testing.T) {
	stranger := signWith(t, "some-other-secret")
	t.Setenv("JWT_SECRET", "new-secret-value")
	t.Setenv("JWT_SECRET_PREVIOUS", "old-secret-value")
	resetJWTSecretsForTest()

	if _, err := ParseHS256(stranger); err == nil {
		t.Fatal("a token signed with an unknown secret verified")
	}
}

func TestSigningSecretIsCurrentOnly(t *testing.T) {
	t.Setenv("JWT_SECRET", "new-secret-value")
	t.Setenv("JWT_SECRET_PREVIOUS", "old-secret-value")
	resetJWTSecretsForTest()

	if string(JWTSecret()) != "new-secret-value" {
		t.Fatalf("JWTSecret() = %q, want the current secret", JWTSecret())
	}
}

func TestParseHS256RejectsNonHMAC(t *testing.T) {
	t.Setenv("JWT_SECRET", "new-secret-value")
	t.Setenv("JWT_SECRET_PREVIOUS", "")
	resetJWTSecretsForTest()

	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "u1"})
	raw, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := ParseHS256(raw); err == nil {
		t.Fatal("alg=none token verified")
	}
}

func TestPreviousSecretsIgnoreBlanksAndDuplicateOfCurrent(t *testing.T) {
	t.Setenv("JWT_SECRET", "new-secret-value")
	t.Setenv("JWT_SECRET_PREVIOUS", " , old-a , ,old-b ")
	resetJWTSecretsForTest()

	got := jwtVerificationSecrets()
	want := []string{"new-secret-value", "old-a", "old-b"}
	if len(got) != len(want) {
		t.Fatalf("got %d secrets, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("secret %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidateJWTSecretRejectsAPlaceholderInPreviousSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "a-real-random-production-secret")
	t.Setenv("JWT_SECRET_PREVIOUS", " "+defaultJWTSecret+" ,another-retired-secret")

	err := ValidateJWTSecret()
	if err == nil {
		t.Fatal("production start accepted a known placeholder in JWT_SECRET_PREVIOUS")
	}
	if !strings.Contains(err.Error(), jwtPreviousSecretEnvVar) {
		t.Fatalf("error does not name the offending variable: %v", err)
	}

	t.Setenv("JWT_SECRET_PREVIOUS", "another-retired-secret")
	if err := ValidateJWTSecret(); err != nil {
		t.Fatalf("a real retired secret was rejected: %v", err)
	}
}

func TestUsingPreviousJWTSecretsReportsAnOpenGraceWindow(t *testing.T) {
	t.Setenv("JWT_SECRET", "current-secret")
	t.Setenv("JWT_SECRET_PREVIOUS", "")
	resetJWTSecretsForTest()
	if UsingPreviousJWTSecrets() {
		t.Fatal("no retired secrets, but a grace window was reported")
	}

	t.Setenv("JWT_SECRET_PREVIOUS", "retired-secret")
	resetJWTSecretsForTest()
	if !UsingPreviousJWTSecrets() {
		t.Fatal("a retired secret is listed but no grace window was reported")
	}
}
