package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/auth"
)

func TestLogoutAuditActorIgnoresASpoofedUserIDHeader(t *testing.T) {
	t.Setenv("JWT_SECRET", "logout-audit-test-secret")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
	if got := verifiedUserIDFromRequest(req); got != "" {
		t.Fatalf("actor = %q, want empty for a request with no verifiable session", got)
	}
}

func TestLogoutAuditActorComesFromAVerifiedToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "logout-audit-test-secret")

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "22222222-2222-2222-2222-222222222222",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
	req.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: signed})

	if got := verifiedUserIDFromRequest(req); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("actor = %q, want the sub of the verified token", got)
	}
}

func TestLogoutAuditActorIgnoresAPendingMFATicket(t *testing.T) {
	t.Setenv("JWT_SECRET", "logout-audit-test-secret")
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "22222222-2222-2222-2222-222222222222",
		"typ": MFATokenType,
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+signed)

	if got := verifiedUserIDFromRequest(req); got != "" {
		t.Fatalf("actor = %q, want empty for a pending second-factor ticket", got)
	}
}
