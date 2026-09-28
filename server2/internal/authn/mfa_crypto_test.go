package authn

import (
	"strings"
	"testing"
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
