// Клиент OIDC (T-029): discovery + JWKS + проверка id_token своими силами на
// crypto/rsa (RS256 — на практике алгоритм подавляющего большинства IdP;
// ES256/HS256 не поддержаны, см. server2/docs/decisions.md, раздел T-029).
// Никакой сторонней библиотеки (github.com/coreos/go-oidc и т.п.) —
// протокол укладывается в несколько HTTP-запросов и одну проверку подписи,
// ровно в духе server2/docs/adr/0001-stack.md.
package authn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// oidcCacheTTL — как долго доверять уже загрученным discovery-документу и
// набору ключей JWKS, не перезапрашивая их у IdP на каждый вход.
const oidcCacheTTL = 10 * time.Minute

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// oidcClient — состояние одного настроенного IdP: HTTP-клиент плюс кэш
// discovery/JWKS. Один экземпляр на процесс (см. Deps.oidc), безопасен для
// конкурентного использования.
type oidcClient struct {
	issuerURL string
	http      *http.Client

	mu          sync.Mutex
	discovery   *oidcDiscovery
	discoveryAt time.Time
	keys        map[string]*rsa.PublicKey
	keysAt      time.Time
}

func newOIDCClient(issuerURL string) *oidcClient {
	return &oidcClient{issuerURL: strings.TrimRight(issuerURL, "/"), http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *oidcClient) discover(ctx context.Context) (oidcDiscovery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discovery != nil && time.Since(c.discoveryAt) < oidcCacheTTL {
		return *c.discovery, nil
	}
	var doc oidcDiscovery
	if err := c.getJSON(ctx, c.issuerURL+"/.well-known/openid-configuration", &doc); err != nil {
		return oidcDiscovery{}, fmt.Errorf("authn: oidc: discovery: %w", err)
	}
	c.discovery = &doc
	c.discoveryAt = time.Now()
	return doc, nil
}

// publicKey возвращает ключ по kid, обновляя JWKS-кэш не чаще раза в oidcCacheTTL
// (и один раз повторно, если запрошенный kid не нашёлся в уже кэшированном
// наборе — типичный признак ротации ключей у IdP).
func (c *oidcClient) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if key, ok := c.cachedKey(kid); ok {
		return key, nil
	}
	if err := c.refreshKeys(ctx); err != nil {
		return nil, err
	}
	if key, ok := c.cachedKey(kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("authn: oidc: неизвестный kid %q в id_token", kid)
}

func (c *oidcClient) cachedKey(kid string) (*rsa.PublicKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil || time.Since(c.keysAt) >= oidcCacheTTL {
		return nil, false
	}
	k, ok := c.keys[kid]
	return k, ok
}

func (c *oidcClient) refreshKeys(ctx context.Context) error {
	disc, err := c.discover(ctx)
	if err != nil {
		return err
	}
	var set jwkSet
	if err := c.getJSON(ctx, disc.JWKSURI, &set); err != nil {
		return fmt.Errorf("authn: oidc: jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	c.mu.Lock()
	c.keys = keys
	c.keysAt = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *oidcClient) getJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("статус %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// exchangeCode — POST на token_endpoint (application/x-www-form-urlencoded,
// grant_type=authorization_code), возвращает сырой id_token.
func (c *oidcClient) exchangeCode(ctx context.Context, code, redirectURI, clientID, clientSecret string) (string, error) {
	disc, err := c.discover(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("authn: oidc: token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("authn: oidc: token endpoint статус %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("authn: oidc: разбор ответа token endpoint: %w", err)
	}
	if tok.IDToken == "" {
		return "", errors.New("authn: oidc: ответ token endpoint не содержит id_token")
	}
	return tok.IDToken, nil
}

// verifyIDToken проверяет подпись id_token и обязательные claim'ы (iss, aud,
// exp) и возвращает разобранные claims — набор возможных полей identity
// (sub/email/name/...) шире, чем стоит типизировать здесь, поэтому claims —
// map, разбор конкретных полей — extractIdentity.
func (c *oidcClient) verifyIDToken(ctx context.Context, idToken, clientID string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("authn: oidc: id_token не в формате JWT")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("authn: oidc: заголовок id_token: %w", err)
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return nil, fmt.Errorf("authn: oidc: заголовок id_token: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("authn: oidc: неподдерживаемый alg %q (нужен RS256)", header.Alg)
	}
	pub, err := c.publicKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("authn: oidc: подпись id_token: %w", err)
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sig); err != nil {
		return nil, fmt.Errorf("authn: oidc: подпись id_token не проходит проверку: %w", err)
	}

	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("authn: oidc: тело id_token: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, fmt.Errorf("authn: oidc: тело id_token: %w", err)
	}

	disc, err := c.discover(ctx)
	if err != nil {
		return nil, err
	}
	if iss, _ := claims["iss"].(string); iss != disc.Issuer {
		return nil, fmt.Errorf("authn: oidc: iss=%q не совпадает с discovery issuer=%q", iss, disc.Issuer)
	}
	if !audienceContains(claims["aud"], clientID) {
		return nil, errors.New("authn: oidc: aud не содержит наш client_id")
	}
	if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
		return nil, errors.New("authn: oidc: id_token истёк")
	}
	return claims, nil
}

func audienceContains(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

func rsaPublicKeyFromJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("jwk n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("jwk e: %w", err)
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("jwk e пуст")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

// extractIdentity сводит claims id_token к (subject, email, displayName).
// trustUnverified — GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL: по умолчанию false,
// но эта версия server2 отклоняет только id_token, где email_verified
// ЯВНО равен false — провайдеры, вовсе не присылающие этот claim (частый
// случай), не наказываются отсутствием того, чего контракт от них не
// требует явно передавать (см. server2/docs/decisions.md, раздел T-026).
func extractIdentity(claims map[string]any, trustUnverified bool) (sub, email, name string, err error) {
	sub, _ = claims["sub"].(string)
	if sub == "" {
		return "", "", "", errors.New("authn: oidc: id_token без sub")
	}
	email, _ = claims["email"].(string)
	email = normalizeEmail(email)
	if !looksLikeEmail(email) {
		return "", "", "", errors.New("authn: oidc: id_token без email")
	}
	if !trustUnverified {
		if v, ok := claims["email_verified"].(bool); ok && !v {
			return "", "", "", errors.New("authn: oidc: email_verified=false, а GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL не включён")
		}
	}
	name, _ = claims["name"].(string)
	if name == "" {
		given, _ := claims["given_name"].(string)
		family, _ := claims["family_name"].(string)
		name = strings.TrimSpace(given + " " + family)
	}
	if name == "" {
		name = displayNameFromEmail(email)
	}
	return sub, email, name, nil
}

// --- state cookie (CSRF/replay) ------------------------------------------------

const (
	oidcStateCookie = "goosar_oidc_state"
	oidcStateTTL    = 10 * time.Minute
)

// signOIDCState упаковывает (state, client, nonce, exp) в значение cookie,
// подписанное HMAC на JWT_SECRET (тот же секрет процесса, что и сессионные
// JWT — отдельного секрета под 10-минутный CSRF-токен контракт не заводит,
// решение этой сессии, см. decisions.md). Формат — "|"-разделённые поля,
// затем "." и подпись, тем же приёмом, что MintDaemonToken/verifyDaemonToken.
func signOIDCState(secret, state, client, nonce string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	payload := strings.Join([]string{state, client, nonce, strconv.FormatInt(exp, 10)}, "|")
	return signHMACPayload(secret, payload)
}

// --- Deps wiring ---------------------------------------------------------------

// oidc возвращает (и лениво создаёт) клиент для d.Config.OIDC.IssuerURL —
// один экземпляр на процесс, переиспользующий кэш discovery/JWKS между
// запросами. Не вызывать, когда IssuerURL пуст (проверка — на вызывающей
// стороне, handleOidcStart/handleOidcCallback ниже).
func (d *Deps) oidc() *oidcClient {
	d.oidcOnce.Do(func() {
		d.oidcInstance = newOIDCClient(d.Config.OIDC.IssuerURL)
	})
	return d.oidcInstance
}

// issueOIDCState генерирует state+nonce и ставит подписанную cookie с ними
// (contract: "Редирект на IdP; ставит подписанную state-cookie") — общая
// подготовка для handleOidcStart, вынесенная сюда, чтобы сам обработчик не
// разбирался в деталях подписи/TTL cookie.
func (d *Deps) issueOIDCState(w http.ResponseWriter, client string) (state, nonce string, err error) {
	state, err = randomToken("", 16)
	if err != nil {
		return "", "", err
	}
	nonce, err = randomToken("", 16)
	if err != nil {
		return "", "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: signOIDCState(d.Config.JWTSecret, state, client, nonce, oidcStateTTL),
		Path: "/", HttpOnly: true, Secure: d.Config.IsProduction(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(oidcStateTTL.Seconds()),
	})
	return state, nonce, nil
}

// checkOIDCCallbackState читает и стирает state-cookie, затем сверяет её
// state с query-параметром ?state= callback'а — оба шага, которые
// handleOidcCallback обязан сделать до чего-либо ещё (contract: state —
// CSRF-защита самого редиректа). Возвращает client ("web"/"desktop", для
// последующих редиректов — даже при ошибке, чтобы редирект на failure ушёл
// туда же, откуда пришёл запрос) и nonce (для resolveOIDCIdentity); errCode
// пусто, если state в порядке.
func (d *Deps) checkOIDCCallbackState(w http.ResponseWriter, r *http.Request) (client, nonce, errCode string) {
	client = "web"
	var wantState string
	var stateOK bool
	if cookie, err := r.Cookie(oidcStateCookie); err == nil {
		if s, c, n, ok := verifyOIDCState(d.Config.JWTSecret, cookie.Value); ok {
			wantState, client, nonce, stateOK = s, c, n, true
		}
	}
	clearOIDCStateCookie(w)

	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	if !stateOK || state == "" || state != wantState || code == "" {
		return client, nonce, "invalid_state"
	}
	return client, nonce, ""
}

// resolveOIDCIdentity — весь путь от "code" из query-параметра callback до
// проверенной identity: обмен на id_token, проверка подписи/claim'ов, сверка
// nonce с тем, что было в state-cookie, разбор sub/email/name. failCode —
// какой auth_error приложить к редиректу на неудаче (handleOidcCallback
// решает, что делать дальше — эта функция только классифицирует причину).
func (d *Deps) resolveOIDCIdentity(ctx context.Context, code, wantNonce string) (sub, email, name, failCode string, err error) {
	idToken, err := d.oidc().exchangeCode(ctx, code, d.oidcRedirectURL(), d.Config.OIDC.ClientID, d.Config.OIDC.ClientSecret)
	if err != nil {
		return "", "", "", "token_exchange_failed", err
	}
	claims, err := d.oidc().verifyIDToken(ctx, idToken, d.Config.OIDC.ClientID)
	if err != nil {
		return "", "", "", "invalid_id_token", err
	}
	if gotNonce, _ := claims["nonce"].(string); gotNonce != wantNonce {
		return "", "", "", "invalid_nonce", fmt.Errorf("authn: oidc: nonce не совпадает со state-cookie")
	}
	sub, email, name, err = extractIdentity(claims, d.Config.OIDC.TrustUnverifiedEmail)
	if err != nil {
		return "", "", "", "missing_email", err
	}
	return sub, email, name, "", nil
}

func verifyOIDCState(secret, cookieValue string) (state, client, nonce string, ok bool) {
	payload, sigOK := verifyHMACPayload(secret, cookieValue)
	if !sigOK {
		return "", "", "", false
	}
	parts := strings.SplitN(payload, "|", 4)
	if len(parts) != 4 {
		return "", "", "", false
	}
	exp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func clearOIDCStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// --- редирект после callback (успех или ошибка) --------------------------------

// finishExternalLogin — общий хвост OIDC (и в будущем — любого другого
// редирект-based провайдера): MFA-гейт, затем редирект с token или mfa_token
// (contract: "результат — HTTP-редирект ..., а не JSON").
func (d *Deps) finishExternalLogin(w http.ResponseWriter, r *http.Request, client string, acct Account) {
	token, mfaRequired, err := d.completeLoginToken(w, r, acct)
	if err != nil {
		d.oidcFailRedirect(w, r, client, "internal_error")
		return
	}
	param := "token"
	if mfaRequired {
		param = "mfa_token"
	}
	d.redirectAfterAuth(w, r, client, param, token)
}

func (d *Deps) oidcFailRedirect(w http.ResponseWriter, r *http.Request, client, code string) {
	http.Redirect(w, r, d.appRedirectBase(client)+"/login#auth_error="+url.QueryEscape(code), http.StatusFound)
}

func (d *Deps) redirectAfterAuth(w http.ResponseWriter, r *http.Request, client, param, value string) {
	http.Redirect(w, r, d.appRedirectBase(client)+"/auth/callback?"+param+"="+url.QueryEscape(value), http.StatusFound)
}

// appRedirectBase — куда вести браузер/десктоп после OIDC (contract: "в
// приложение или goosar:// для десктопа"). Десктоп — фиксированная схема
// goosar://; веб — FRONTEND_ORIGIN без хвостового слэша.
func (d *Deps) appRedirectBase(client string) string {
	if client == "desktop" {
		return "goosar://"
	}
	return strings.TrimRight(d.Config.FrontendOrigin, "/")
}

// oidcRedirectURL — OIDC_REDIRECT_URL, если задан, иначе строится из
// GOOSAR_PUBLIC_URL (contract §1.9: "базовый публичный адрес сервера"); ни
// один из них не задан — пустая строка (IdP отклонит запрос с пустым
// redirect_uri, что честно отражает "сервер не настроен до конца", см.
// decisions.md).
func (d *Deps) oidcRedirectURL() string {
	if d.Config.OIDC.RedirectURL != "" {
		return d.Config.OIDC.RedirectURL
	}
	if d.Config.PublicURL == "" {
		return ""
	}
	return strings.TrimRight(d.Config.PublicURL, "/") + "/api/auth/oidc/callback"
}

// --- HTTP-обработчики ------------------------------------------------------

// handleOidcStart — GET /api/auth/oidc/start: редирект на authorization_endpoint
// IdP с подписанной state-cookie (contract: "Редирект на IdP; ставит
// подписанную state-cookie"). Ответы этой операции в контракте — только
// 302/404/500 (нет 429): при исчерпанном лимите эта ручка всё равно отвечает
// 429 JSON — контракт группирует её лимит с verify-code/verify-link/mfa-verify
// под одним RATE_LIMIT_AUTH_VERIFY (§1.5), и оставить его молча
// неисполняемым здесь было бы дырой в защите, которую §1.5 явно требует (см.
// server2/docs/decisions.md, раздел T-029).
func (d *Deps) handleOidcStart(w http.ResponseWriter, r *http.Request) {
	if !d.Config.AuthMethodEnabled("oidc") || d.Config.OIDC.IssuerURL == "" {
		httpapi.WriteError(w, http.StatusNotFound, "OIDC not enabled on this server", "oidc_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Enforce(w, ip, "too many requests") {
		return
	}

	client := "web"
	if r.URL.Query().Get("client") == "desktop" {
		client = "desktop"
	}
	disc, err := d.oidc().discover(r.Context())
	if err != nil {
		d.Logger.Error("auth: oidc: discovery", "err", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to prepare state", "internal_error")
		return
	}
	state, nonce, err := d.issueOIDCState(w, client)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "failed to prepare state", "internal_error")
		return
	}

	q := url.Values{
		"response_type": {"code"},
		"client_id":     {d.Config.OIDC.ClientID},
		"redirect_uri":  {d.oidcRedirectURL()},
		"scope":         {strings.Join(d.Config.OIDC.Scopes, " ")}, // GOOSAR_OIDC_SCOPES
		"state":         {state},
		"nonce":         {nonce},
	}
	http.Redirect(w, r, disc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
}

// handleOidcCallback — GET /api/auth/oidc/callback: обмен code на identity,
// дальше как verify-code, но результат — редирект (contract §3.3/§3.6:
// "Always responds with an HTTP redirect"). Раз контракт прямо обещает
// редирект даже на ошибку, rate limit здесь тоже отвечает редиректом с
// auth_error=rate_limited, а не голым 429 (в отличие от oidc/start, чей
// список ответов не содержит прозы "always a redirect").
func (d *Deps) handleOidcCallback(w http.ResponseWriter, r *http.Request) {
	client, nonce, stateErr := d.checkOIDCCallbackState(w, r)
	if !d.Config.AuthMethodEnabled("oidc") || d.Config.OIDC.IssuerURL == "" {
		httpapi.WriteError(w, http.StatusNotFound, "OIDC not enabled on this server", "oidc_not_enabled")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.AuthVerifyIPLimiter.Allow(ip) {
		d.oidcFailRedirect(w, r, client, "rate_limited")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		d.oidcFailRedirect(w, r, client, "oidc_"+errParam)
		return
	}
	if stateErr != "" {
		d.oidcFailRedirect(w, r, client, stateErr)
		return
	}

	sub, email, name, failCode, err := d.resolveOIDCIdentity(r.Context(), r.URL.Query().Get("code"), nonce)
	if err != nil {
		d.oidcAbort(w, r, client, "обмен кода/идентификация", failCode, err)
		return
	}
	acct, err := d.loginOrLinkExternal(r, "oidc", sub, email, name)
	if errors.Is(err, errEmailNotAllowed) {
		d.oidcFailRedirect(w, r, client, "email_not_allowed")
		return
	}
	if err != nil {
		d.oidcAbort(w, r, client, "вход/создание аккаунта", "internal_error", err)
		return
	}
	d.finishExternalLogin(w, r, client, acct)
}

// oidcAbort логирует причину неудачи одной строкой и уводит редиректом на
// /login#auth_error=<failCode> — общий финал для обоих шагов после
// checkOIDCCallbackState, которым нужно и то, и другое.
func (d *Deps) oidcAbort(w http.ResponseWriter, r *http.Request, client, step, failCode string, err error) {
	d.Logger.Error("auth: oidc: "+step, "err", err)
	d.oidcFailRedirect(w, r, client, failCode)
}
