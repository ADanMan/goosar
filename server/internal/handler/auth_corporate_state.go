package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/adanman/goosar/server/internal/auth"
)

const (
	oidcStateCookieName = "goosar_oidc_state"

	oidcStateTTL = 10 * time.Minute

	oidcStateTokenType = "oidc_state"
)

const (
	oidcClientWeb     = "web"
	oidcClientDesktop = "desktop"
)

var errOIDCState = errors.New("corporate sign-in: authorization state did not verify")

type oidcState struct {
	State    string
	Nonce    string
	Verifier string

	Client string
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newOIDCState(client string) (oidcState, error) {
	var (
		st  oidcState
		err error
	)
	if st.State, err = randomURLSafe(32); err != nil {
		return oidcState{}, err
	}
	if st.Nonce, err = randomURLSafe(32); err != nil {
		return oidcState{}, err
	}
	if st.Verifier, err = randomURLSafe(32); err != nil {
		return oidcState{}, err
	}
	st.Client = client
	return st, nil
}

func (h *Handler) setOIDCStateCookie(w http.ResponseWriter, st oidcState) error {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"typ": oidcStateTokenType,
		"st":  st.State,
		"non": st.Nonce,
		"ver": st.Verifier,
		"cl":  st.Client,
		"exp": time.Now().Add(oidcStateTTL).Unix(),
		"iat": time.Now().Unix(),
	})
	signed, err := token.SignedString(auth.JWTSecret())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    signed,
		Path:     "/api/auth/oidc",
		Domain:   auth.CookieDomain(),
		MaxAge:   int(oidcStateTTL.Seconds()),
		Expires:  time.Now().Add(oidcStateTTL),
		HttpOnly: true,
		Secure:   auth.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (h *Handler) clearOIDCStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     "/api/auth/oidc",
		Domain:   auth.CookieDomain(),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   auth.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) readOIDCState(r *http.Request, echoed string) (oidcState, error) {
	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil || cookie.Value == "" {
		return oidcState{}, errOIDCState
	}
	token, err := auth.ParseHS256(cookie.Value)
	if err != nil || !token.Valid {
		return oidcState{}, errOIDCState
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return oidcState{}, errOIDCState
	}
	if typ, _ := claims["typ"].(string); typ != oidcStateTokenType {
		return oidcState{}, errOIDCState
	}
	st := oidcState{}
	st.State, _ = claims["st"].(string)
	st.Nonce, _ = claims["non"].(string)
	st.Verifier, _ = claims["ver"].(string)
	st.Client, _ = claims["cl"].(string)
	if st.State == "" || st.Nonce == "" || st.Verifier == "" {
		return oidcState{}, errOIDCState
	}

	if subtle.ConstantTimeCompare([]byte(st.State), []byte(echoed)) != 1 {
		return oidcState{}, errOIDCState
	}
	return st, nil
}
