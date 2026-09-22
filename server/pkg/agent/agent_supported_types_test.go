package agent

import (
	"log/slog"
	"testing"
)

func TestSupportedTypesLockstepWithNew(t *testing.T) {
	cfg := Config{Logger: slog.Default()}

	for _, typ := range SupportedTypes {
		if !IsSupportedType(typ) {
			t.Errorf("IsSupportedType(%q) = false, but it is in SupportedTypes", typ)
		}
		if _, err := New(typ, cfg); err != nil {
			t.Errorf("New(%q) returned error for a SupportedTypes entry: %v", typ, err)
		}
	}

	const bogus = "definitely-not-a-real-backend"
	if IsSupportedType(bogus) {
		t.Errorf("IsSupportedType(%q) = true, want false", bogus)
	}
	if _, err := New(bogus, cfg); err == nil {
		t.Errorf("New(%q) succeeded, want error for an unsupported type", bogus)
	}
}

func TestSupportedTypesMatchesMigrationWhitelist(t *testing.T) {
	want := map[string]bool{
		"runtime-a": true, "runtime-c": true,
		"runtime-d": true, "runtime-e": true, "runtime-f": true,
		"runtime-g": true, "runtime-h": true, "runtime-i": true,
		"runtime-j": true, "runtime-k": true, "runtime-l": true,
		"runtime-m": true, "runtime-n": true, "runtime-o": true,
		"runtime-p": true, "runtime-q": true, "runtime-r": true,
	}
	if len(SupportedTypes) != len(want) {
		t.Fatalf("SupportedTypes has %d entries, migration whitelist has %d; keep them in lockstep", len(SupportedTypes), len(want))
	}
	for _, typ := range SupportedTypes {
		if !want[typ] {
			t.Errorf("SupportedTypes contains %q which is not in the migration 120 protocol_family CHECK", typ)
		}
	}
}
