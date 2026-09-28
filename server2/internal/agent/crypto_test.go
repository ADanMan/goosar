package agent

import "testing"

func TestSealOpenJSON_RoundTrip(t *testing.T) {
	type payload struct {
		A string `json:"a"`
		B int    `json:"b"`
	}
	in := payload{A: "secret", B: 42}

	sealed, encrypted, err := sealJSON("k1", in)
	if err != nil {
		t.Fatalf("sealJSON: %v", err)
	}
	if !encrypted {
		t.Fatal("expected encrypted=true when key is set")
	}

	var out payload
	if !openJSON("k1", "", sealed, &out) {
		t.Fatal("openJSON with the same key should succeed")
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestSealJSON_EmptyKeyStoresPlaintext(t *testing.T) {
	sealed, encrypted, err := sealJSON("", map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("sealJSON: %v", err)
	}
	if encrypted {
		t.Fatal("expected encrypted=false when key is empty")
	}
	var out map[string]string
	if !openJSON("", "", sealed, &out) {
		t.Fatal("openJSON should read plaintext back without a key")
	}
	if out["x"] != "y" {
		t.Fatalf("got %v", out)
	}
}

func TestOpenJSON_KeyRotationFallsBackToPrevious(t *testing.T) {
	sealed, _, err := sealJSON("old-key", map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("sealJSON: %v", err)
	}
	// текущий ключ сменился, но GOOSAR_MCP_SECRET_KEY_PREVIOUS ещё несёт старый.
	var out map[string]string
	if !openJSON("new-key", "old-key", sealed, &out) {
		t.Fatal("openJSON should fall back to the previous key")
	}
	if out["x"] != "y" {
		t.Fatalf("got %v", out)
	}
}

func TestOpenJSON_WrongKeyFails(t *testing.T) {
	sealed, _, err := sealJSON("k1", map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("sealJSON: %v", err)
	}
	var out map[string]string
	if openJSON("k2", "k3", sealed, &out) {
		t.Fatal("openJSON must fail when neither key matches")
	}
}

func TestBrokenMaskValue(t *testing.T) {
	cases := map[string]bool{
		"****":    false, // маркер целиком — не испорченный ввод
		"****xyz": true,  // испорченный маркер
		"hello":   false,
		"":        false,
	}
	for in, want := range cases {
		if got := BrokenMaskValue(in); got != want {
			t.Errorf("BrokenMaskValue(%q) = %v, want %v", in, got, want)
		}
	}
}
