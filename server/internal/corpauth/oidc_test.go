package corpauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testNonce = "nonce-for-this-request"

func TestOIDCExchange_VerifiesAssertionAndReturnsIdentity(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", testNonce)
	p := iss.providerFor(t, nil)
	verifier := randomVerifier(t)

	identity, err := p.Exchange(context.Background(), "auth-code", verifier, testNonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if identity.Provider != MethodOIDC {
		t.Errorf("Provider: got %q, want %q", identity.Provider, MethodOIDC)
	}

	if want := iss.URL + "|user-subject-1"; identity.Subject != want {
		t.Errorf("Subject: got %q, want %q", identity.Subject, want)
	}

	if identity.Email != "person@example.test" {
		t.Errorf("Email: got %q, want the address folded to lower case", identity.Email)
	}
	if identity.Name != "Test Person" {
		t.Errorf("Name: got %q", identity.Name)
	}
	if identity.IsAdmin {
		t.Error("IsAdmin must be false with no admin mapping configured")
	}
}

func TestOIDCExchange_SendsPKCEVerifier(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", testNonce)
	p := iss.providerFor(t, nil)
	verifier := randomVerifier(t)

	if _, err := p.Exchange(context.Background(), "auth-code", verifier, testNonce); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got := iss.lastForm.Get("code_verifier"); got != verifier {
		t.Fatalf("code_verifier: got %q, want the verifier this flow minted", got)
	}
}

func TestOIDCAuthCodeURL_PinsS256AndCarriesNonce(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	p := iss.providerFor(t, nil)

	raw, err := p.AuthCodeURL(context.Background(), "state-value", testNonce, randomVerifier(t))
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method: got %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge missing: PKCE is not being applied")
	}
	if q.Get("nonce") != testNonce {
		t.Errorf("nonce: got %q, want %q", q.Get("nonce"), testNonce)
	}
	if q.Get("state") != "state-value" {
		t.Errorf("state: got %q", q.Get("state"))
	}
}

func TestOIDCExchange_RefusesMismatchedNonce(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", "nonce-from-some-other-sign-in")
	p := iss.providerFor(t, nil)

	_, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
	if strings.Contains(err.Error(), "eyJ") {
		t.Fatal("the error carries a JWT: this string is logged, and an id_token is a credential")
	}
}

func TestOIDCExchange_RefusesAbsentNonce(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", nil)
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestOIDCExchange_RefusesExpiredToken(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = func() map[string]any {
		return map[string]any{
			"iss": iss.URL, "aud": "goosar", "sub": "user-subject-1",
			"email": "person@example.test", "nonce": testNonce,
			"exp": time.Now().Add(-time.Hour).Unix(),
			"iat": time.Now().Add(-2 * time.Hour).Unix(),
		}
	}
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestOIDCExchange_RefusesUnknownIssuer(t *testing.T) {
	ours := newTestIssuer(t, "goosar")
	stranger := newTestIssuer(t, "goosar")

	ours.claims = func() map[string]any {
		c := stranger.claims()
		c["nonce"] = testNonce
		return c
	}
	ours.key, ours.keyID = stranger.key, stranger.keyID

	p := ours.providerFor(t, nil)
	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid for a token from a foreign issuer", err)
	}
}

func TestOIDCExchange_RefusesWrongAudience(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("aud", "some-other-application")
	iss.claims = iss.withClaim("nonce", testNonce)
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestOIDCExchange_RefusesAssertionWithoutEmail(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", testNonce)
	iss.claims = iss.withClaim("email", nil)
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrNoEmail) {
		t.Fatalf("got %v, want ErrNoEmail", err)
	}
}

func TestOIDCExchange_RefusesResponseWithoutIDToken(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.omitIDToken = true
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}

func TestOIDC_ProviderDownIsNamedNotFatal(t *testing.T) {
	t.Run("discovery unreachable", func(t *testing.T) {

		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		addr := dead.URL
		dead.Close()

		p := NewOIDCProvider(OIDCConfig{
			Issuer: addr, ClientID: "goosar",
			RedirectURL: "https://goosar.example.test/cb",
		})
		_, err := p.AuthCodeURL(context.Background(), "s", "n", randomVerifier(t))
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("got %v, want ErrUnavailable", err)
		}
	})

	t.Run("token endpoint refuses", func(t *testing.T) {
		iss := newTestIssuer(t, "goosar")
		iss.tokenStatus = http.StatusBadGateway
		p := iss.providerFor(t, nil)

		if _, err := p.Exchange(context.Background(), "code", randomVerifier(t), testNonce); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("got %v, want ErrUnavailable", err)
		}
	})
}

func TestOIDC_DiscoveryIsCachedAndSurvivesABlip(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	p := iss.providerFor(t, nil)

	if _, err := p.AuthCodeURL(context.Background(), "s", "n", randomVerifier(t)); err != nil {
		t.Fatalf("first AuthCodeURL: %v", err)
	}

	iss.Close()
	if _, err := p.AuthCodeURL(context.Background(), "s", "n", randomVerifier(t)); err != nil {
		t.Fatalf("second AuthCodeURL after the IdP went away: %v", err)
	}
}

func TestOIDC_AdminClaimMapping(t *testing.T) {
	cases := []struct {
		name       string
		claimName  string
		claimValue any
		cfgClaim   string
		cfgValue   string
		want       bool
	}{
		{"list contains the group", "groups", []string{"eng", "hermes-admins"}, "groups", "hermes-admins", true},
		{"single string matches", "role", "hermes-admins", "role", "hermes-admins", true},
		{"case folded", "groups", []string{"Hermes-Admins"}, "groups", "hermes-admins", true},
		{"list without the group", "groups", []string{"eng", "sales"}, "groups", "hermes-admins", false},
		{"claim absent", "groups", nil, "groups", "hermes-admins", false},
		{"no mapping configured", "groups", []string{"hermes-admins"}, "", "", false},

		{"value not configured", "groups", []string{"hermes-admins"}, "groups", "", false},

		{"unreadable claim shape", "groups", map[string]any{"nested": true}, "groups", "hermes-admins", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iss := newTestIssuer(t, "goosar")
			iss.claims = iss.withClaim("nonce", testNonce)
			iss.claims = iss.withClaim(tc.claimName, tc.claimValue)
			p := iss.providerFor(t, func(c *OIDCConfig) {
				c.AdminClaim, c.AdminValue = tc.cfgClaim, tc.cfgValue
			})

			identity, err := p.Exchange(context.Background(), "code", randomVerifier(t), testNonce)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if identity.IsAdmin != tc.want {
				t.Fatalf("IsAdmin: got %v, want %v", identity.IsAdmin, tc.want)
			}
		})
	}
}

func TestOIDCConfig_Configured(t *testing.T) {
	full := OIDCConfig{Issuer: "https://i.example.test", ClientID: "c", RedirectURL: "https://s.example.test/cb"}
	if !full.Configured() {
		t.Fatal("a complete configuration must be Configured()")
	}
	for _, missing := range []struct {
		name string
		cfg  OIDCConfig
	}{
		{"issuer", OIDCConfig{ClientID: "c", RedirectURL: "u"}},
		{"client id", OIDCConfig{Issuer: "i", RedirectURL: "u"}},
		{"redirect", OIDCConfig{Issuer: "i", ClientID: "c"}},
	} {
		t.Run("missing "+missing.name, func(t *testing.T) {
			if missing.cfg.Configured() {
				t.Fatal("incomplete configuration reported as Configured()")
			}
			if NewOIDCProvider(missing.cfg) != nil {
				t.Fatal("NewOIDCProvider built a provider from an incomplete configuration")
			}
		})
	}
}

func TestOIDCConfigFromEnv_AlwaysRequestsOpenIDScope(t *testing.T) {
	t.Setenv(OIDCIssuerEnvVar, "https://sso.example.test")
	t.Setenv(OIDCClientIDEnvVar, "goosar")
	t.Setenv(OIDCRedirectURLEnvVar, "https://goosar.example.test/cb")

	t.Run("defaults", func(t *testing.T) {
		t.Setenv(OIDCScopesEnvVar, "")
		if got := OIDCConfigFromEnv().Scopes; !contains(got, "openid") {
			t.Fatalf("scopes: got %v, want openid included", got)
		}
	})
	t.Run("operator omitted it", func(t *testing.T) {
		t.Setenv(OIDCScopesEnvVar, "profile, email")
		got := OIDCConfigFromEnv().Scopes
		if !contains(got, "openid") {
			t.Fatalf("scopes: got %v, want openid added", got)
		}
		if !contains(got, "profile") || !contains(got, "email") {
			t.Fatalf("scopes: got %v, want the operator's own scopes kept", got)
		}
	})
}

func contains(list []string, want string) bool { return containsFold(list, want) }

func TestOIDCConfigFromEnv_ReadsSecretFromEnvironment(t *testing.T) {
	t.Setenv(OIDCIssuerEnvVar, "https://sso.example.test")
	t.Setenv(OIDCClientIDEnvVar, "goosar")
	t.Setenv(OIDCRedirectURLEnvVar, "https://goosar.example.test/cb")
	t.Setenv(OIDCClientSecretEnvVar, "s3cr3t-value")

	if got := OIDCConfigFromEnv().ClientSecret; got != "s3cr3t-value" {
		t.Fatalf("ClientSecret: got %q", got)
	}
}

func TestOIDCExchange_ErrorsNeverCarryTheToken(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testIssuer)
	}{
		{"expired", func(i *testIssuer) {
			i.claims = func() map[string]any {
				return map[string]any{
					"iss": i.URL, "aud": "goosar", "sub": "user-subject-1",
					"email": "person@example.test", "nonce": testNonce,
					"exp": time.Now().Add(-time.Hour).Unix(),
				}
			}
		}},
		{"wrong audience", func(i *testIssuer) {
			i.claims = i.withClaim("aud", "some-other-application")
		}},
		{"nonce mismatch", func(i *testIssuer) {
			i.claims = i.withClaim("nonce", "a-different-request")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iss := newTestIssuer(t, "goosar")
			tc.mutate(iss)
			p := iss.providerFor(t, nil)

			_, err := p.Exchange(context.Background(), "code", randomVerifier(t), testNonce)
			if err == nil {
				t.Fatal("expected a verification failure")
			}

			if strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("the error carries a JWT, which will be logged: %v", err)
			}
		})
	}
}

func TestOIDCExchange_RefusesUnverifiedEmail(t *testing.T) {
	for _, claim := range []any{false, "false"} {
		iss := newTestIssuer(t, "goosar")
		iss.claims = iss.withClaim("nonce", testNonce)
		iss.claims = iss.withClaim("email_verified", claim)
		p := iss.providerFor(t, nil)

		_, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce)
		if !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("email_verified=%v: got %v, want ErrTokenInvalid", claim, err)
		}
	}
}

func TestOIDCExchange_AcceptsVerifiedOrAbsentEmailClaim(t *testing.T) {
	for _, claim := range []any{nil, true, "true"} {
		iss := newTestIssuer(t, "goosar")
		iss.claims = iss.withClaim("nonce", testNonce)
		if claim != nil {
			iss.claims = iss.withClaim("email_verified", claim)
		}
		p := iss.providerFor(t, nil)

		identity, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce)
		if err != nil {
			t.Fatalf("email_verified=%v: %v", claim, err)
		}
		if identity.Email != "person@example.test" {
			t.Fatalf("email_verified=%v: email %q", claim, identity.Email)
		}
	}
}

func TestOIDCConfig_RequiresEncryptedIssuer(t *testing.T) {
	cases := []struct {
		issuer string
		want   bool
	}{
		{"https://idp.example.test", true},
		{"http://idp.example.test", false},
		{"http://localhost:8080/realms/goosar", true},
		{"http://127.0.0.1:8080", true},
		{"ldap://idp.example.test", false},
		{"", false},
	}
	for _, tc := range cases {
		cfg := OIDCConfig{Issuer: tc.issuer, ClientID: "id", RedirectURL: "https://goosar.example.test/cb"}
		if got := cfg.EncryptedIssuer(); got != tc.want {
			t.Errorf("EncryptedIssuer(%q): got %v, want %v", tc.issuer, got, tc.want)
		}
		if provider := NewOIDCProvider(cfg); (provider != nil) != tc.want {
			t.Errorf("NewOIDCProvider(%q): offered=%v, want %v", tc.issuer, provider != nil, tc.want)
		}
	}
}

func TestOIDCExchange_RefusesAssertionWithoutSubject(t *testing.T) {
	iss := newTestIssuer(t, "goosar")
	iss.claims = iss.withClaim("nonce", testNonce)
	iss.claims = iss.withClaim("sub", nil)
	p := iss.providerFor(t, nil)

	if _, err := p.Exchange(context.Background(), "auth-code", randomVerifier(t), testNonce); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("got %v, want ErrTokenInvalid", err)
	}
}
