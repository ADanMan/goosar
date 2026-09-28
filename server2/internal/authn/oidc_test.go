package authn

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeOIDCProvider — сервер httptest, отдающий discovery-документ, JWKS с
// одним RSA-ключом и подписывающий id_token этим же ключом на /token —
// ровно то, что нужно, чтобы протестировать oidcClient.verifyIDToken/
// exchangeCode без настоящего IdP (contract требует "тест на
// httptest-провайдере").
type fakeOIDCProvider struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	issuer string
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	p := &fakeOIDCProvider{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 p.issuer,
			"authorization_endpoint": p.issuer + "/authorize",
			"token_endpoint":         p.issuer + "/token",
			"jwks_uri":               p.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": p.kid,
				"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(bigEndianForInt(key.PublicKey.E)),
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idToken := p.mustSignIDToken(t, map[string]any{
			"iss":   p.issuer,
			"aud":   "test-client",
			"sub":   "user-123",
			"email": "alice@example.test",
			"name":  "Alice Example",
			"nonce": r.URL.Query().Get("nonce_echo"),
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
		})
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken, "access_token": "at-1"})
	})
	p.srv = httptest.NewServer(mux)
	p.issuer = p.srv.URL
	t.Cleanup(p.srv.Close)
	return p
}

func bigEndianForInt(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	var b []byte
	for v := n; v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	return b
}

func (p *fakeOIDCProvider) mustSignIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": p.kid}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestOIDCDiscoverAndVerifyIDToken(t *testing.T) {
	p := newFakeOIDCProvider(t)
	client := newOIDCClient(p.issuer)

	disc, err := client.discover(t.Context())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if disc.TokenEndpoint != p.issuer+"/token" {
		t.Errorf("token_endpoint = %q", disc.TokenEndpoint)
	}

	idToken := p.mustSignIDToken(t, map[string]any{
		"iss": p.issuer, "aud": "test-client", "sub": "user-123",
		"email": "alice@example.test", "name": "Alice Example",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	claims, err := client.verifyIDToken(t.Context(), idToken, "test-client")
	if err != nil {
		t.Fatalf("verifyIDToken: %v", err)
	}
	sub, email, name, err := extractIdentity(claims)
	if err != nil {
		t.Fatalf("extractIdentity: %v", err)
	}
	if sub != "user-123" || email != "alice@example.test" || name != "Alice Example" {
		t.Errorf("identity = (%q, %q, %q)", sub, email, name)
	}
}

func TestOIDCVerifyIDTokenRejectsWrongAudience(t *testing.T) {
	p := newFakeOIDCProvider(t)
	client := newOIDCClient(p.issuer)
	if _, err := client.discover(t.Context()); err != nil {
		t.Fatalf("discover: %v", err)
	}
	idToken := p.mustSignIDToken(t, map[string]any{
		"iss": p.issuer, "aud": "someone-else", "sub": "user-123",
		"email": "alice@example.test", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client.verifyIDToken(t.Context(), idToken, "test-client"); err == nil {
		t.Fatal("verifyIDToken: expected an error for a mismatched audience")
	}
}

func TestOIDCVerifyIDTokenRejectsExpired(t *testing.T) {
	p := newFakeOIDCProvider(t)
	client := newOIDCClient(p.issuer)
	if _, err := client.discover(t.Context()); err != nil {
		t.Fatalf("discover: %v", err)
	}
	idToken := p.mustSignIDToken(t, map[string]any{
		"iss": p.issuer, "aud": "test-client", "sub": "user-123",
		"email": "alice@example.test", "exp": time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := client.verifyIDToken(t.Context(), idToken, "test-client"); err == nil {
		t.Fatal("verifyIDToken: expected an error for an expired token")
	}
}

func TestOIDCVerifyIDTokenRejectsBadSignature(t *testing.T) {
	p := newFakeOIDCProvider(t)
	client := newOIDCClient(p.issuer)
	if _, err := client.discover(t.Context()); err != nil {
		t.Fatalf("discover: %v", err)
	}
	idToken := p.mustSignIDToken(t, map[string]any{
		"iss": p.issuer, "aud": "test-client", "sub": "user-123",
		"email": "alice@example.test", "exp": time.Now().Add(time.Hour).Unix(),
	})
	tampered := idToken[:len(idToken)-4] + "abcd"
	if _, err := client.verifyIDToken(t.Context(), tampered, "test-client"); err == nil {
		t.Fatal("verifyIDToken: expected an error for a tampered signature")
	}
}

func TestOIDCExchangeCode(t *testing.T) {
	p := newFakeOIDCProvider(t)
	client := newOIDCClient(p.issuer)
	idToken, err := client.exchangeCode(t.Context(), "auth-code-1", "https://app.test/callback", "test-client", "shh")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	claims, err := client.verifyIDToken(t.Context(), idToken, "test-client")
	if err != nil {
		t.Fatalf("verifyIDToken on exchanged token: %v", err)
	}
	if claims["sub"] != "user-123" {
		t.Errorf("sub = %v", claims["sub"])
	}
}

func TestOIDCStateSignAndVerifyRoundTrip(t *testing.T) {
	tok := signOIDCState("secret", "state-1", "desktop", "nonce-1", time.Minute)
	state, client, nonce, ok := verifyOIDCState("secret", tok)
	if !ok {
		t.Fatal("verifyOIDCState: expected ok=true")
	}
	if state != "state-1" || client != "desktop" || nonce != "nonce-1" {
		t.Errorf("got (%q, %q, %q)", state, client, nonce)
	}
	if _, _, _, ok := verifyOIDCState("wrong-secret", tok); ok {
		t.Fatal("verifyOIDCState: expected ok=false with the wrong secret")
	}
}

func TestOIDCStateExpires(t *testing.T) {
	tok := signOIDCState("secret", "s", "web", "n", -time.Minute)
	if _, _, _, ok := verifyOIDCState("secret", tok); ok {
		t.Fatal("verifyOIDCState: expected an expired state to fail")
	}
}
