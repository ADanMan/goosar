package authn

import (
	"strings"
	"testing"
	"time"
)

// jwtCase — один сценарий Sign/Verify: check получает выпущенный токен (уже
// возможно испорченный/подписанный другим ключом верификатором vf) и решает,
// прошёл тест или нет. Табличная форма вместо отдельной функции на сценарий —
// сознательно другая структура, чем «одна функция — один assert».
type jwtCase struct {
	name   string
	claims Claims
	ttl    time.Duration
	mutate func(token string) string
	wantOK bool
}

func TestSignerScenarios(t *testing.T) {
	baseSecret := "test-secret"
	cases := []jwtCase{
		{
			name:   "round-trip",
			claims: Claims{Sub: "u1", Email: "a@example.test", Name: "Alice", TV: 2, SID: "s1"},
			ttl:    time.Hour,
			wantOK: true,
		},
		{
			name:   "tampered signature",
			claims: Claims{Sub: "u1"},
			ttl:    time.Hour,
			mutate: func(tok string) string { return tok[:len(tok)-1] + "x" },
			wantOK: false,
		},
		{
			name:   "expired",
			claims: Claims{Sub: "u1"},
			ttl:    -time.Minute,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := NewSigner(baseSecret)
			token, err := signer.Sign(tc.claims, tc.ttl)
			if err != nil {
				t.Fatalf("Sign вернул ошибку: %v", err)
			}
			if tc.mutate != nil {
				token = tc.mutate(token)
			}
			got, err := signer.Verify(token)
			ok := err == nil
			if ok != tc.wantOK {
				t.Fatalf("case %s: Verify ok=%v (err=%v), хотели %v", tc.name, ok, err, tc.wantOK)
			}
			if ok && got.Sub != tc.claims.Sub {
				t.Fatalf("case %s: sub=%q, хотели %q", tc.name, got.Sub, tc.claims.Sub)
			}
		})
	}
}

func TestSignerCrossSecretMismatch(t *testing.T) {
	token, err := NewSigner("secret-a").Sign(Claims{Sub: "cross"}, time.Hour)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	for _, secret := range []string{"secret-b", "secret-c", ""} {
		if _, err := NewSigner(secret).Verify(token); err == nil {
			t.Errorf("секрет %q не должен был принять чужой токен", secret)
		}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("токен должен состоять из трёх частей через точку, получено %d", len(parts))
	}
}

func TestRandomCodeIsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		code, err := randomCode(6)
		if err != nil {
			t.Fatalf("randomCode: %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("длина кода = %d, хотели 6 (%q)", len(code), code)
		}
		seen[code] = true
	}
	if len(seen) < 2 {
		t.Fatalf("20 генераций дали только %d уникальных значений — подозрительно не случайно", len(seen))
	}
}

func TestDigestAndConstantTimeEqual(t *testing.T) {
	x, y, z := digest("value-a"), digest("value-a"), digest("value-b")
	switch {
	case x != y:
		t.Fatal("digest недетерминирован для одного и того же входа")
	case !constantTimeEqual(x, y):
		t.Fatal("constantTimeEqual(равные) должно быть true")
	case constantTimeEqual(x, z):
		t.Fatal("constantTimeEqual(разные) должно быть false")
	}
}
