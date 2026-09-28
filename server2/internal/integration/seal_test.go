package integration

import (
	"testing"
	"time"
)

func TestSealUnsealRoundtrip(t *testing.T) {
	sealed, err := seal("k1", []byte("secret-token"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) == "secret-token" {
		t.Fatal("sealed value must not equal plaintext when a key is set")
	}
	plain, ok := unseal("k1", "", sealed)
	if !ok || string(plain) != "secret-token" {
		t.Fatalf("unseal with current key: ok=%v plain=%q", ok, plain)
	}
}

func TestUnsealWithPreviousKeyDuringRotation(t *testing.T) {
	sealed, err := seal("old-key", []byte("secret-token"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	plain, ok := unseal("new-key", "old-key", sealed)
	if !ok || string(plain) != "secret-token" {
		t.Fatalf("unseal with previous key: ok=%v plain=%q", ok, plain)
	}
	if _, ok := unseal("new-key", "", sealed); ok {
		t.Fatal("unseal must fail without the right key")
	}
}

func TestSealWithoutKeyStoresPlaintext(t *testing.T) {
	sealed, err := seal("", []byte("plain"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) != "plain" {
		t.Fatalf("expected plaintext passthrough, got %q", sealed)
	}
	plain, ok := unseal("", "", sealed)
	if !ok || string(plain) != "plain" {
		t.Fatalf("unseal passthrough: ok=%v plain=%q", ok, plain)
	}
}

func TestStateTicket(t *testing.T) {
	const secret = "s3cr3t-state-key"

	t.Run("roundtrip carries string fields", func(t *testing.T) {
		ticket, err := signState(secret, map[string]any{"workspace_id": "ws-1", "return_to": "github"}, time.Minute)
		if err != nil {
			t.Fatalf("signState: %v", err)
		}
		fields, err := verifyState(secret, ticket)
		if err != nil {
			t.Fatalf("verifyState: %v", err)
		}
		if fields["workspace_id"] != "ws-1" || fields["return_to"] != "github" {
			t.Fatalf("fields lost in roundtrip: %#v", fields)
		}
	})

	t.Run("non-string fields are dropped, not preserved as-is", func(t *testing.T) {
		ticket, err := signState(secret, map[string]any{"kept": "v", "dropped": 42}, time.Minute)
		if err != nil {
			t.Fatalf("signState: %v", err)
		}
		fields, err := verifyState(secret, ticket)
		if err != nil {
			t.Fatalf("verifyState: %v", err)
		}
		if _, present := fields["dropped"]; present {
			t.Fatalf("expected numeric field dropped, got %#v", fields)
		}
		if fields["kept"] != "v" {
			t.Fatalf("expected kept field, got %#v", fields)
		}
	})

	t.Run("expired ticket is rejected with errStateExpired", func(t *testing.T) {
		ticket, err := signState(secret, map[string]any{"x": "y"}, -time.Second)
		if err != nil {
			t.Fatalf("signState: %v", err)
		}
		if _, err := verifyState(secret, ticket); err != errStateExpired {
			t.Fatalf("want errStateExpired, got %v", err)
		}
	})

	badTickets := map[string]struct {
		key   string
		token func(valid string) string
	}{
		"wrong key":          {key: "another-key", token: func(v string) string { return v }},
		"trailing junk":      {key: secret, token: func(v string) string { return v + "AA" }},
		"empty string":       {key: secret, token: func(string) string { return "" }},
		"not base64url data": {key: secret, token: func(string) string { return "###" }},
	}
	valid, err := signState(secret, map[string]any{"a": "b"}, time.Minute)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	for name, tc := range badTickets {
		t.Run(name, func(t *testing.T) {
			if _, err := verifyState(tc.key, tc.token(valid)); err == nil {
				t.Fatal("expected verification to fail")
			}
		})
	}
}
