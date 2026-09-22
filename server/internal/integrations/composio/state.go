package composio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrStateMalformed = errors.New("composio: state malformed")

	ErrStateSignature = errors.New("composio: state signature mismatch")

	ErrStateExpired = errors.New("composio: state expired")
)

type stateClaims struct {
	UserID      string `json:"u"`
	ToolkitSlug string `json:"t"`

	AuthConfigID string `json:"a"`
	Exp          int64  `json:"e"`
}

func signState(secret []byte, claims stateClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := signPayload(secret, payload)
	return payload + "." + sig, nil
}

func verifyState(secret []byte, token string, now time.Time) (stateClaims, error) {
	payload, sig, found := strings.Cut(token, ".")
	if !found || payload == "" || sig == "" {
		return stateClaims{}, ErrStateMalformed
	}
	expected := signPayload(secret, payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return stateClaims{}, ErrStateSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return stateClaims{}, ErrStateMalformed
	}
	var claims stateClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return stateClaims{}, ErrStateMalformed
	}
	if now.Unix() > claims.Exp {
		return stateClaims{}, ErrStateExpired
	}
	return claims, nil
}

func signPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
