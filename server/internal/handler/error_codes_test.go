package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

func codeTestBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return &buf
}

func TestWriteErrorCodeKeepsEnglishMessage(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Request-ID", "req-1")
	writeErrorCode(w, http.StatusForbidden, ErrCodeSignupDisabled, "user registration is disabled")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want 403", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "user registration is disabled" {
		t.Fatalf("error: got %q", body["error"])
	}
	if body["code"] != "signup_disabled" {
		t.Fatalf("code: got %q", body["code"])
	}
	if body["request_id"] != "req-1" {
		t.Fatalf("request_id: got %q", body["request_id"])
	}
}

func TestSignupErrorsCarryDistinctCodes(t *testing.T) {
	if ErrSignupProhibited.Code != ErrCodeSignupDisabled {
		t.Fatalf("ErrSignupProhibited.Code: got %q", ErrSignupProhibited.Code)
	}
	if ErrEmailNotAllowed.Code != ErrCodeEmailDomainNotAllows {
		t.Fatalf("ErrEmailNotAllowed.Code: got %q", ErrEmailNotAllowed.Code)
	}
	if ErrSignupProhibited.Error() != "user registration is disabled on this self-hosted instance" {
		t.Fatalf("ErrSignupProhibited message drifted: %q", ErrSignupProhibited.Error())
	}
	if ErrEmailNotAllowed.Error() != "email address or domain not allowed on this instance" {
		t.Fatalf("ErrEmailNotAllowed message drifted: %q", ErrEmailNotAllowed.Error())
	}
}

func TestVerifyCodeRefusalCarriesCode(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/verify-code",
		codeTestBody(t, map[string]string{"email": "nobody-430@example.com", "code": "000000"}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	testHandler.VerifyCode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != ErrCodeInvalidCode {
		t.Fatalf("code: got %q, want %q", body["code"], ErrCodeInvalidCode)
	}
	if body["error"] != "invalid or expired code" {
		t.Fatalf("error message drifted: %q", body["error"])
	}
}

func TestEveryErrorCodeIsLocalisedByTheClient(t *testing.T) {
	const (
		goSource = "error_codes.go"
		tsSource = "../../../packages/views/common/server-error.ts"
	)

	declared, err := os.ReadFile(goSource)
	if err != nil {
		t.Fatalf("read %s: %v", goSource, err)
	}
	mapping, err := os.ReadFile(tsSource)
	if err != nil {
		t.Fatalf("read %s: %v", tsSource, err)
	}

	slugs := regexp.MustCompile(`ErrCode\w+\s*=\s*"([a-z0-9_]+)"`).FindAllStringSubmatch(string(declared), -1)
	if len(slugs) == 0 {
		t.Fatal("no error codes found; did the const block change shape?")
	}

	for _, m := range slugs {
		slug := m[1]

		pattern := regexp.MustCompile(`case ["']` + regexp.QuoteMeta(slug) + `["']:`)
		if !pattern.MatchString(string(mapping)) {
			t.Errorf("error code %q has no case in %s: a client would show the English sentence instead of localized copy", slug, tsSource)
		}
	}
}
