package corpauth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type testIssuer struct {
	*httptest.Server
	key      *rsa.PrivateKey
	keyID    string
	clientID string

	claims func() map[string]any

	tokenStatus int

	omitIDToken bool

	lastForm url.Values
}

func newTestIssuer(t *testing.T, clientID string) *testIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	iss := &testIssuer{key: key, keyID: "test-key-1", clientID: clientID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                iss.URL,
			"authorization_endpoint":                iss.URL + "/authorize",
			"token_endpoint":                        iss.URL + "/token",
			"jwks_uri":                              iss.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     iss.keyID,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		iss.lastForm = r.PostForm
		if iss.tokenStatus != 0 {
			w.WriteHeader(iss.tokenStatus)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		body := map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if !iss.omitIDToken {
			body["id_token"] = iss.signIDToken(t)
		}
		writeJSON(w, body)
	})

	iss.Server = httptest.NewServer(mux)
	t.Cleanup(iss.Close)

	iss.claims = func() map[string]any {
		return map[string]any{
			"iss":   iss.URL,
			"aud":   clientID,
			"sub":   "user-subject-1",
			"email": "Person@example.test",
			"name":  "Test Person",
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": "",
		}
	}
	return iss
}

func (iss *testIssuer) signIDToken(t *testing.T) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: iss.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", iss.keyID),
	)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	raw, err := jwt.Signed(signer).Claims(iss.claims()).Serialize()
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return raw
}

func (iss *testIssuer) withClaim(key string, value any) func() map[string]any {
	base := iss.claims
	return func() map[string]any {
		c := base()
		if value == nil {
			delete(c, key)
			return c
		}
		c[key] = value
		return c
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (iss *testIssuer) providerFor(t *testing.T, mutate func(*OIDCConfig)) *OIDCProvider {
	t.Helper()
	cfg := OIDCConfig{
		Issuer:      iss.URL,
		ClientID:    iss.clientID,
		RedirectURL: "https://goosar.example.test/api/auth/oidc/callback",
		Scopes:      []string{"openid", "profile", "email"},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	p := NewOIDCProvider(cfg)
	if p == nil {
		t.Fatal("NewOIDCProvider returned nil for a complete configuration")
	}
	return p
}

func randomVerifier(t *testing.T) string {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(n.String() + "0123456789abcdefghijklmnop"))
}
