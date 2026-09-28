package seal

import "testing"

func TestSealOpen_roundTrip(t *testing.T) {
	key := "test-key-0123456789abcdef"
	ciphertext, err := Seal(key, []byte("hello secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	plain, ok := Open(key, "", ciphertext)
	if !ok {
		t.Fatal("Open with the same key should succeed")
	}
	if string(plain) != "hello secret" {
		t.Errorf("Open = %q, want %q", plain, "hello secret")
	}
}

func TestSeal_requiresKey(t *testing.T) {
	if _, err := Seal("", []byte("x")); err != ErrUnavailable {
		t.Errorf("Seal with empty key: err = %v, want ErrUnavailable", err)
	}
}

func TestOpen_rotationFallsBackToPreviousKey(t *testing.T) {
	oldKey, newKey := "old-key-0123456789", "new-key-9876543210"
	ciphertext, err := Seal(oldKey, []byte("rotate me"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Ключ поменялся; старое значение ещё не перешифровано (rotate-secrets ещё
	// не пробегал) — Open должен достать его через prevKey.
	plain, ok := Open(newKey, oldKey, ciphertext)
	if !ok {
		t.Fatal("Open should fall back to the previous key")
	}
	if string(plain) != "rotate me" {
		t.Errorf("Open = %q, want %q", plain, "rotate me")
	}
	// Ни текущий, ни "предыдущий" (тоже неверный) — не открывается.
	if _, ok := Open(newKey, "another-wrong-key", ciphertext); ok {
		t.Error("Open should fail when neither key matches")
	}
}

func TestOpen_emptyCiphertextIsOK(t *testing.T) {
	plain, ok := Open("any-key", "", nil)
	if !ok || len(plain) != 0 {
		t.Errorf("Open(nil) = (%q, %v), want (\"\", true)", plain, ok)
	}
}

func TestSealJSON_OpenJSON_roundTrip(t *testing.T) {
	key := "json-roundtrip-key"
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	sealed, err := SealJSON(key, payload{Name: "jira", N: 7})
	if err != nil {
		t.Fatalf("SealJSON: %v", err)
	}
	var out payload
	if !OpenJSON(key, "", sealed, &out) {
		t.Fatal("OpenJSON should succeed with the right key")
	}
	if out.Name != "jira" || out.N != 7 {
		t.Errorf("OpenJSON = %+v, want {jira 7}", out)
	}
}

func TestAvailable(t *testing.T) {
	if Available("") {
		t.Error("Available(\"\") = true, want false")
	}
	if !Available("k") {
		t.Error("Available(\"k\") = false, want true")
	}
}

func TestHashJSON_isStableAndDeterministic(t *testing.T) {
	a := HashJSON(map[string]int{"x": 1})
	b := HashJSON(map[string]int{"x": 1})
	if a == "" || a != b {
		t.Errorf("HashJSON should be deterministic and non-empty: a=%q b=%q", a, b)
	}
	c := HashJSON(map[string]int{"x": 2})
	if c == a {
		t.Error("HashJSON should differ for different input")
	}
}
