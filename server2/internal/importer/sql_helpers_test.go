package importer

import "testing"

func TestJsonb_nilBecomesEmptyObject(t *testing.T) {
	got, err := jsonb(nil)
	if err != nil {
		t.Fatalf("jsonb(nil): %v", err)
	}
	if got != "{}" {
		t.Errorf("jsonb(nil) = %q, want {}", got)
	}
}

func TestJsonb_marshalsMap(t *testing.T) {
	got, err := jsonb(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("jsonb: %v", err)
	}
	if got != `{"a":1}` {
		t.Errorf("jsonb(map) = %q, want {\"a\":1}", got)
	}
}

func TestJsonbArray_nilBecomesEmptyArray(t *testing.T) {
	got, err := jsonbArray(nil)
	if err != nil {
		t.Fatalf("jsonbArray(nil): %v", err)
	}
	if got != "[]" {
		t.Errorf("jsonbArray(nil) = %q, want []", got)
	}
}

func TestJsonbArray_marshalsSlice(t *testing.T) {
	got, err := jsonbArray([]string{"a", "b"})
	if err != nil {
		t.Fatalf("jsonbArray: %v", err)
	}
	if got != `["a","b"]` {
		t.Errorf("jsonbArray(slice) = %q, want [\"a\",\"b\"]", got)
	}
}

func TestStrPtrOrEmpty(t *testing.T) {
	if got := strPtrOrEmpty(nil); got != "" {
		t.Errorf("strPtrOrEmpty(nil) = %q, want empty", got)
	}
	s := "2024-01-02"
	if got := strPtrOrEmpty(&s); got != s {
		t.Errorf("strPtrOrEmpty(&s) = %q, want %q", got, s)
	}
}

func TestIndexLabelsByID(t *testing.T) {
	labels := []sourceLabel{
		{ID: "a", Name: "bug"},
		{ID: "b", Name: "feature"},
	}
	idx := indexLabelsByID(labels)
	if len(idx) != 2 {
		t.Fatalf("len(idx) = %d, want 2", len(idx))
	}
	if idx["a"].Name != "bug" {
		t.Errorf("idx[a].Name = %q, want bug", idx["a"].Name)
	}
	if _, ok := idx["missing"]; ok {
		t.Error("idx[missing] should not exist")
	}
}

func TestMapVisibility_isIdentity(t *testing.T) {
	for _, v := range []string{"private", "public"} {
		if got := mapVisibility(v); got != v {
			t.Errorf("mapVisibility(%q) = %q, want identity", v, got)
		}
	}
}
