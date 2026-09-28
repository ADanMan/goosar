package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Signer подписывает и проверяет сессионные JWT HS256 вручную поверх
// crypto/hmac — см. server2/docs/adr/0001-stack.md за обоснование выбора
// (не тянуть golang-jwt ради одного алгоритма с фиксированным набором
// клеймов, который контракт описывает явно: sub, email, name, tv, sid, iat, exp).
type Signer struct {
	secret []byte
}

func NewSigner(secret string) *Signer { return &Signer{secret: []byte(secret)} }

// Claims — набор полей сессионного JWT, ровно как их описывает
// components/securitySchemes/cookieAuth в контракте.
type Claims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	TV    int    `json:"tv"`  // token version (acct_token_epoch на момент выпуска)
	SID   string `json:"sid"` // login_sessions.id
	IAT   int64  `json:"iat"`
	EXP   int64  `json:"exp"`
}

var jwtHeader = base64URL([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Sign выпускает подписанный токен, действительный ttl от текущего момента.
func (s *Signer) Sign(c Claims, ttl time.Duration) (string, error) {
	now := time.Now()
	c.IAT = now.Unix()
	c.EXP = now.Add(ttl).Unix()
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("authn: маршалинг claims: %w", err)
	}
	signingInput := jwtHeader + "." + base64URL(payload)
	sig := s.sign(signingInput)
	return signingInput + "." + sig, nil
}

// Verify проверяет подпись и срок действия и возвращает claims.
func (s *Signer) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("authn: неверный формат токена")
	}
	signingInput := parts[0] + "." + parts[1]
	expected := s.sign(signingInput)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return Claims{}, fmt.Errorf("authn: неверная подпись токена")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("authn: декодирование payload: %w", err)
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, fmt.Errorf("authn: разбор claims: %w", err)
	}
	if time.Now().Unix() > c.EXP {
		return Claims{}, fmt.Errorf("authn: токен истёк")
	}
	return c, nil
}

func (s *Signer) sign(signingInput string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(signingInput))
	return base64URL(mac.Sum(nil))
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
