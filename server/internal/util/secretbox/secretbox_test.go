package secretbox

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func mustNewBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	box, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return box
}

func TestRoundTrip(t *testing.T) {
	box := mustNewBox(t)
	plaintext := []byte("lark app_secret 12345")
	sealed, err := box.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("round trip mismatch: got %q want %q", opened, plaintext)
	}
}

func TestSealIsNonDeterministic(t *testing.T) {

	box := mustNewBox(t)
	plaintext := []byte("repeat")
	a, _ := box.Seal(plaintext)
	b, _ := box.Seal(plaintext)
	if bytes.Equal(a, b) {
		t.Fatalf("expected non-deterministic Seal, got identical ciphertexts")
	}
}

func TestOpenRejectsTampered(t *testing.T) {
	box := mustNewBox(t)
	sealed, _ := box.Seal([]byte("important"))

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := box.Open(tampered); err == nil {
		t.Fatalf("expected auth failure on tampered ciphertext")
	}
}

func TestOpenRejectsShort(t *testing.T) {
	box := mustNewBox(t)
	if _, err := box.Open([]byte("short")); err != ErrCiphertextTooShort {
		t.Fatalf("expected ErrCiphertextTooShort, got %v", err)
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(make([]byte, 16)); err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for 16-byte key, got %v", err)
	}
}

func TestLoadKey(t *testing.T) {
	const envVar = "TEST_SECRETBOX_KEY"
	t.Run("missing", func(t *testing.T) {
		t.Setenv(envVar, "")
		if _, err := LoadKey(envVar); err == nil {
			t.Fatal("expected error on missing env var")
		}
	})
	t.Run("bad base64", func(t *testing.T) {
		t.Setenv(envVar, "not!base64!")
		if _, err := LoadKey(envVar); err == nil {
			t.Fatal("expected error on invalid base64")
		}
	})
	t.Run("wrong length", func(t *testing.T) {
		t.Setenv(envVar, base64.StdEncoding.EncodeToString([]byte("too short")))
		if _, err := LoadKey(envVar); err == nil {
			t.Fatal("expected error on short key")
		}
	})
	t.Run("happy path", func(t *testing.T) {
		key := make([]byte, KeySize)
		_, _ = rand.Read(key)
		t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
		got, err := LoadKey(envVar)
		if err != nil {
			t.Fatalf("LoadKey: %v", err)
		}
		if !bytes.Equal(got, key) {
			t.Fatalf("LoadKey returned wrong bytes")
		}
	})
}

func TestLoadKeyErrKeyNotSet(t *testing.T) {
	const envVar = "TEST_SECRETBOX_KEY_NOTSET"
	t.Run("missing wraps ErrKeyNotSet", func(t *testing.T) {
		t.Setenv(envVar, "")
		_, err := LoadKey(envVar)
		if !errors.Is(err, ErrKeyNotSet) {
			t.Fatalf("expected errors.Is(err, ErrKeyNotSet), got %v", err)
		}
	})
	t.Run("bad base64 does not wrap ErrKeyNotSet", func(t *testing.T) {
		t.Setenv(envVar, "not!base64!")
		_, err := LoadKey(envVar)
		if errors.Is(err, ErrKeyNotSet) {
			t.Fatalf("bad base64 must not be classified as ErrKeyNotSet, got %v", err)
		}
		if !strings.Contains(err.Error(), "not valid base64") {
			t.Fatalf("expected error to name the real cause, got %v", err)
		}
	})
	t.Run("wrong length does not wrap ErrKeyNotSet", func(t *testing.T) {
		t.Setenv(envVar, base64.StdEncoding.EncodeToString([]byte("too short")))
		_, err := LoadKey(envVar)
		if errors.Is(err, ErrKeyNotSet) {
			t.Fatalf("wrong length must not be classified as ErrKeyNotSet, got %v", err)
		}
		if !strings.Contains(err.Error(), "expected 32") {
			t.Fatalf("expected error to name the real cause, got %v", err)
		}
	})
}

func TestValidateKeyEnv(t *testing.T) {
	const envVar = "TEST_SECRETBOX_VALIDATE_KEY"
	validKey := make([]byte, KeySize)
	if _, err := rand.Read(validKey); err != nil {
		t.Fatalf("rand: %v", err)
	}
	validB64 := base64.StdEncoding.EncodeToString(validKey)

	tests := []struct {
		name      string
		appEnv    string
		keyValue  string
		wantErr   bool
		errSubstr string
	}{
		{name: "unset key, production, starts", appEnv: "production", keyValue: "", wantErr: false},
		{name: "unset key, dev, starts", appEnv: "development", keyValue: "", wantErr: false},
		{name: "unset key, unset APP_ENV, starts", appEnv: "", keyValue: "", wantErr: false},
		{name: "bad base64, production, refuses", appEnv: "production", keyValue: "not!base64!", wantErr: true, errSubstr: "not valid base64"},
		{name: "bad base64, dev, starts (loud log is the caller's job)", appEnv: "development", keyValue: "not!base64!", wantErr: false},
		{name: "bad base64, case-insensitive APP_ENV match, refuses", appEnv: "Production", keyValue: "not!base64!", wantErr: true, errSubstr: "not valid base64"},
		{name: "wrong length, production, refuses", appEnv: "production", keyValue: base64.StdEncoding.EncodeToString([]byte("too short")), wantErr: true, errSubstr: "expected 32"},
		{name: "wrong length, dev, starts", appEnv: "development", keyValue: base64.StdEncoding.EncodeToString([]byte("too short")), wantErr: false},
		{name: "valid key, production, starts", appEnv: "production", keyValue: validB64, wantErr: false},
		{name: "valid key, dev, starts", appEnv: "development", keyValue: validB64, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tt.appEnv)
			t.Setenv(envVar, tt.keyValue)

			err := ValidateKeyEnv(envVar)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected refusal error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error to contain %q, got %v", tt.errSubstr, err)
				}
				if !strings.Contains(err.Error(), envVar) {
					t.Fatalf("refusal error must name the env var, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected startup allowed, got error: %v", err)
			}
		})
	}
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return key
}

func TestRingOpensCiphertextSealedWithPreviousKey(t *testing.T) {

	oldKey, newKey := mustKey(t), mustKey(t)
	oldBox, err := New(oldKey)
	if err != nil {
		t.Fatalf("New(old): %v", err)
	}
	sealed, err := oldBox.Seal([]byte("mcp gateway token"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	ring, err := NewRing(newKey, [][]byte{oldKey})
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	opened, err := ring.Open(sealed)
	if err != nil {
		t.Fatalf("ring Open: %v", err)
	}
	if !bytes.Equal(opened, []byte("mcp gateway token")) {
		t.Fatalf("got %q", opened)
	}
}

func TestRingSealsWithCurrentKeyOnly(t *testing.T) {

	oldKey, newKey := mustKey(t), mustKey(t)
	ring, err := NewRing(newKey, [][]byte{oldKey})
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	sealed, err := ring.Seal([]byte("payload"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	currentOnly, err := New(newKey)
	if err != nil {
		t.Fatalf("New(new): %v", err)
	}
	if _, err := currentOnly.Open(sealed); err != nil {
		t.Fatalf("current-key box could not open ring output: %v", err)
	}
	oldOnly, err := New(oldKey)
	if err != nil {
		t.Fatalf("New(old): %v", err)
	}
	if _, err := oldOnly.Open(sealed); err == nil {
		t.Fatal("retired key opened a freshly sealed payload — Seal used the wrong key")
	}
}

func TestRingRejectsUnknownKey(t *testing.T) {
	strangerBox, err := New(mustKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := strangerBox.Seal([]byte("x"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	ring, err := NewRing(mustKey(t), [][]byte{mustKey(t)})
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	if _, err := ring.Open(sealed); err == nil {
		t.Fatal("ring opened a payload sealed with a key it does not hold")
	}
}

func TestSealedWithCurrentKey(t *testing.T) {

	oldKey, newKey := mustKey(t), mustKey(t)
	oldBox, _ := New(oldKey)
	ring, err := NewRing(newKey, [][]byte{oldKey})
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	stale, _ := oldBox.Seal([]byte("v"))
	fresh, _ := ring.Seal([]byte("v"))
	if ring.SealedWithCurrentKey(stale) {
		t.Fatal("payload on the retired key reported as current")
	}
	if !ring.SealedWithCurrentKey(fresh) {
		t.Fatal("payload on the current key reported as stale")
	}
}

func TestLoadKeyRingFromEnv(t *testing.T) {
	current, prev1, prev2 := mustKey(t), mustKey(t), mustKey(t)
	t.Setenv("TEST_RING_KEY", base64.StdEncoding.EncodeToString(current))
	t.Setenv("TEST_RING_KEY_PREVIOUS", base64.StdEncoding.EncodeToString(prev1)+", "+base64.StdEncoding.EncodeToString(prev2))

	box, err := FromEnv("TEST_RING_KEY")
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	for i, key := range [][]byte{current, prev1, prev2} {
		b, _ := New(key)
		sealed, _ := b.Seal([]byte("v"))
		if _, err := box.Open(sealed); err != nil {
			t.Fatalf("key %d not in the ring: %v", i, err)
		}
	}
}

func TestFromEnvRejectsMalformedPreviousKey(t *testing.T) {

	t.Setenv("TEST_RING_KEY", base64.StdEncoding.EncodeToString(mustKey(t)))
	t.Setenv("TEST_RING_KEY_PREVIOUS", "not-base64!!")
	if _, err := FromEnv("TEST_RING_KEY"); err == nil {
		t.Fatal("FromEnv accepted a malformed *_PREVIOUS entry")
	}
}

func TestFromEnvUnsetCurrentKeyStaysErrKeyNotSet(t *testing.T) {
	t.Setenv("TEST_RING_KEY", "")
	t.Setenv("TEST_RING_KEY_PREVIOUS", "")
	_, err := FromEnv("TEST_RING_KEY")
	if !errors.Is(err, ErrKeyNotSet) {
		t.Fatalf("want ErrKeyNotSet, got %v", err)
	}
}
