package corpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	OIDCIssuerEnvVar       = "GOOSAR_OIDC_ISSUER"
	OIDCClientIDEnvVar     = "GOOSAR_OIDC_CLIENT_ID"
	OIDCClientSecretEnvVar = "GOOSAR_OIDC_CLIENT_SECRET"
	OIDCRedirectURLEnvVar  = "GOOSAR_OIDC_REDIRECT_URL"
	OIDCScopesEnvVar       = "GOOSAR_OIDC_SCOPES"
	OIDCAdminClaimEnvVar   = "GOOSAR_OIDC_ADMIN_CLAIM"
	OIDCAdminValueEnvVar   = "GOOSAR_OIDC_ADMIN_VALUE"
	OIDCDisplayNameEnvVar  = "GOOSAR_OIDC_DISPLAY_NAME"
)

const discoveryTTL = 15 * time.Minute

const discoveryTimeout = 10 * time.Second

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string

	AdminClaim string
	AdminValue string

	DisplayName string
}

func (c OIDCConfig) Configured() bool {
	return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != ""
}

func (c OIDCConfig) EncryptedIssuer() bool {
	u, err := url.Parse(c.Issuer)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Scheme, "https") {
		return true
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func OIDCConfigFromEnv() OIDCConfig {
	scopes := splitList(os.Getenv(OIDCScopesEnvVar))
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	} else if !containsFold(scopes, oidc.ScopeOpenID) {

		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return OIDCConfig{
		Issuer:       strings.TrimSpace(os.Getenv(OIDCIssuerEnvVar)),
		ClientID:     strings.TrimSpace(os.Getenv(OIDCClientIDEnvVar)),
		ClientSecret: os.Getenv(OIDCClientSecretEnvVar),
		RedirectURL:  strings.TrimSpace(os.Getenv(OIDCRedirectURLEnvVar)),
		Scopes:       scopes,
		AdminClaim:   strings.TrimSpace(os.Getenv(OIDCAdminClaimEnvVar)),
		AdminValue:   strings.TrimSpace(os.Getenv(OIDCAdminValueEnvVar)),
		DisplayName:  strings.TrimSpace(os.Getenv(OIDCDisplayNameEnvVar)),
	}
}

func splitList(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

type Identity struct {
	Provider string

	Subject string
	Email   string
	Name    string

	EmailVerified bool

	IsAdmin bool
}

type OIDCProvider struct {
	cfg    OIDCConfig
	client *http.Client

	mu        sync.Mutex
	provider  *oidc.Provider
	fetchedAt time.Time
}

func NewOIDCProvider(cfg OIDCConfig) *OIDCProvider {
	if !cfg.Configured() || !cfg.EncryptedIssuer() {
		return nil
	}
	return &OIDCProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: discoveryTimeout},
	}
}

func (p *OIDCProvider) Config() OIDCConfig {
	if p == nil {
		return OIDCConfig{}
	}
	return p.cfg
}

func (p *OIDCProvider) resolve(ctx context.Context) (*oidc.Provider, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider != nil && time.Since(p.fetchedAt) < discoveryTTL {
		return p.provider, nil
	}

	ctx = oidc.ClientContext(ctx, p.client)
	provider, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		if p.provider != nil {
			return p.provider, nil
		}
		return nil, fmt.Errorf("%w: discovery: %v", ErrUnavailable, err)
	}
	p.provider = provider
	p.fetchedAt = time.Now()
	return provider, nil
}

func (p *OIDCProvider) oauthConfig(provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  p.cfg.RedirectURL,
		Scopes:       p.cfg.Scopes,
	}
}

func (p *OIDCProvider) AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	provider, err := p.resolve(ctx)
	if err != nil {
		return "", err
	}
	return p.oauthConfig(provider).AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), nil
}

func (p *OIDCProvider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	provider, err := p.resolve(ctx)
	if err != nil {
		return Identity{}, err
	}

	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.oauthConfig(provider).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {

		return Identity{}, fmt.Errorf("%w: token exchange: %v", ErrUnavailable, err)
	}

	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		return Identity{}, fmt.Errorf("%w: response carried no id_token", ErrTokenInvalid)
	}

	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {

		return Identity{}, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if idToken.Nonce != nonce {
		return Identity{}, fmt.Errorf("%w: nonce mismatch", ErrTokenInvalid)
	}

	var claims map[string]json.RawMessage
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", ErrTokenInvalid, err)
	}

	email := strings.ToLower(strings.TrimSpace(stringClaim(claims, "email")))
	if email == "" {
		return Identity{}, ErrNoEmail
	}

	verified, present := boolClaim(claims, "email_verified")
	if present && !verified {
		return Identity{}, fmt.Errorf("%w: the provider reports the address as unverified", ErrTokenInvalid)
	}

	if strings.TrimSpace(idToken.Subject) == "" {
		return Identity{}, fmt.Errorf("%w: assertion carries no subject", ErrTokenInvalid)
	}
	name := strings.TrimSpace(stringClaim(claims, "name"))
	if name == "" {
		name = strings.TrimSpace(stringClaim(claims, "preferred_username"))
	}

	return Identity{
		Provider: MethodOIDC,

		Subject:       idToken.Issuer + "|" + idToken.Subject,
		Email:         email,
		Name:          name,
		EmailVerified: present && verified,
		IsAdmin:       p.claimGrantsAdmin(claims),
	}, nil
}

func (p *OIDCProvider) claimGrantsAdmin(claims map[string]json.RawMessage) bool {
	if p.cfg.AdminClaim == "" || p.cfg.AdminValue == "" {
		return false
	}
	raw, ok := claims[p.cfg.AdminClaim]
	if !ok {
		return false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return strings.EqualFold(single, p.cfg.AdminValue)
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return containsFold(list, p.cfg.AdminValue)
	}
	return false
}

func boolClaim(claims map[string]json.RawMessage, key string) (value, present bool) {
	raw, ok := claims[key]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}

	return false, false
}

func stringClaim(claims map[string]json.RawMessage, key string) string {
	raw, ok := claims[key]
	if !ok {
		return ""
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v
}

func IsConfigError(err error) bool { return errors.Is(err, ErrNotConfigured) }
