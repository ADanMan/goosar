package authn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adanman/goosar/server2/internal/config"
)

func TestSealUnsealSecret(t *testing.T) {
	secret, _ := newTOTPSecret()

	t.Run("round trip with the same key", func(t *testing.T) {
		sealed, err := sealSecret("k1", secret)
		if err != nil {
			t.Fatalf("sealSecret: %v", err)
		}
		got, err := unsealSecret("k1", "", sealed)
		if err != nil || string(got) != string(secret) {
			t.Fatalf("unsealSecret = (%q, %v), want (%q, nil)", got, err, secret)
		}
	})

	t.Run("rotation falls back to the previous key", func(t *testing.T) {
		sealed, err := sealSecret("old-key", secret)
		if err != nil {
			t.Fatalf("sealSecret: %v", err)
		}
		if got, err := unsealSecret("new-key", "old-key", sealed); err != nil || string(got) != string(secret) {
			t.Fatalf("unsealSecret с prevKey = (%q, %v), want (%q, nil)", got, err, secret)
		}
		if _, err := unsealSecret("new-key", "", sealed); err == nil {
			t.Fatal("без prevKey расшифровка неверным ключом должна проваливаться")
		}
	})

	t.Run("requires a non-empty key", func(t *testing.T) {
		if _, err := sealSecret("", secret); err == nil {
			t.Fatal("sealSecret с пустым ключом должен вернуть ошибку")
		}
	})
}

func TestNewRecoveryCodesAreUniqueAndFormatted(t *testing.T) {
	codes, err := newRecoveryCodes(recoveryCodeCount)
	if err != nil {
		t.Fatalf("newRecoveryCodes: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("len(codes) = %d, want %d", len(codes), recoveryCodeCount)
	}
	seen := make(map[string]bool, len(codes))
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("повторяющийся recovery-код: %q", c)
		}
		seen[c] = true
		if len(c) != 9 || c[4] != '-' {
			t.Errorf("код %q не соответствует формату XXXX-XXXX", c)
		}
		if normalizeRecoveryCode(strings.ToLower(c)) != c {
			t.Errorf("normalizeRecoveryCode(%q) должен вернуть исходный код в верхнем регистре", c)
		}
	}
}

// TestConsumeTOTPStep_rejectsReplayWithinWindow — T-029 доводка: один и тот
// же принятый TOTP-шаг (или более ранний) отклоняется второй раз, даже если
// математически код всё ещё валиден в пределах окна ±1 (RFC 6238).
func TestConsumeTOTPStep_rejectsReplayWithinWindow(t *testing.T) {
	db := newTestDB(t)
	s := &Store{db: db}
	ctx := context.Background()
	accountID := seedAccount(t, db)
	if err := s.UpsertPendingFactor(ctx, accountID, []byte("sealed-secret")); err != nil {
		t.Fatalf("UpsertPendingFactor: %v", err)
	}

	ok, err := s.ConsumeTOTPStep(ctx, accountID, 1000)
	if err != nil || !ok {
		t.Fatalf("первое предъявление шага 1000 должно приняться: ok=%v err=%v", ok, err)
	}
	// Тот же шаг повторно — отклонён (replay).
	ok, err = s.ConsumeTOTPStep(ctx, accountID, 1000)
	if err != nil || ok {
		t.Fatalf("повторное предъявление шага 1000 должно отклоняться: ok=%v err=%v", ok, err)
	}
	// Более ранний шаг (в пределах окна ±1, но уже "в прошлом" относительно
	// принятого) — тоже отклонён.
	ok, err = s.ConsumeTOTPStep(ctx, accountID, 999)
	if err != nil || ok {
		t.Fatalf("более ранний шаг должен отклоняться: ok=%v err=%v", ok, err)
	}
	// Более новый шаг — принимается.
	ok, err = s.ConsumeTOTPStep(ctx, accountID, 1001)
	if err != nil || !ok {
		t.Fatalf("более новый шаг должен приниматься: ok=%v err=%v", ok, err)
	}
}

// TestVerifyTOTPCode_rejectsReplay — сквозной сценарий через тот же путь, что
// authn/handlers_mfa.go: одинаковый код (валидный весь TOTP-период ±1 шаг)
// принимается один раз, повторное предъявление отклоняется.
func TestVerifyTOTPCode_rejectsReplay(t *testing.T) {
	db := newTestDB(t)
	s := &Store{db: db}
	d := &Deps{Store: s, Config: config.Config{McpSecretKey: "test-mcp-secret-key-0123456789"}}
	ctx := context.Background()
	accountID := seedAccount(t, db)

	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("newTOTPSecret: %v", err)
	}
	sealed, err := sealSecret(d.Config.McpSecretKey, secret)
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}
	if err := s.UpsertPendingFactor(ctx, accountID, sealed); err != nil {
		t.Fatalf("UpsertPendingFactor: %v", err)
	}

	code := hotp(secret, uint64(totpCounter(time.Now())))
	ok, err := d.verifyTOTPCode(ctx, accountID, sealed, code)
	if err != nil || !ok {
		t.Fatalf("первая проверка кода должна пройти: ok=%v err=%v", ok, err)
	}
	ok, err = d.verifyTOTPCode(ctx, accountID, sealed, code)
	if err != nil || ok {
		t.Fatalf("повторная проверка того же кода должна отклоняться (anti-replay): ok=%v err=%v", ok, err)
	}
}
