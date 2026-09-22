package provisioning

import (
	"context"
	"errors"
	"sort"
	"testing"
)

type fakeCatalog map[string]PackageManifest

func (c fakeCatalog) lookup(_ context.Context, name, version string) (PackageManifest, error) {
	m, ok := c[name+"@"+version]
	if !ok {
		return PackageManifest{}, ErrPackageNotFound
	}
	return m, nil
}

func mustManifest(t *testing.T, pkgType, name, version, platform string, requires ...string) PackageManifest {
	t.Helper()
	return PackageManifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          name,
		Version:       version,
		Type:          pkgType,
		Platform:      platform,
		SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:          1,
		Requires:      requires,
	}
}

func keysOf(manifests []PackageManifest) []string {
	keys := make([]string, len(manifests))
	for i, m := range manifests {
		keys[i] = m.RequireKey()
	}
	sort.Strings(keys)
	return keys
}

func TestResolveRequires_Flattening(t *testing.T) {
	runtime := mustManifest(t, PackageTypeRuntime, "playwright-browsers", "2.0.0", "*")
	mcp := mustManifest(t, PackageTypeMCPServer, "office", "1.0.0", "linux-x64", "runtime:playwright-browsers@2.0.0")
	skillA := mustManifest(t, PackageTypeSkill, "office-docx", "1.4.0", "*", "runtime:playwright-browsers@2.0.0", "mcp-server:office@1.0.0")
	skillB := mustManifest(t, PackageTypeSkill, "office-xlsx", "1.0.0", "*", "runtime:playwright-browsers@2.0.0")

	catalog := fakeCatalog{
		"playwright-browsers@2.0.0": runtime,
		"office@1.0.0":              mcp,
		"office-docx@1.4.0":         skillA,
		"office-xlsx@1.0.0":         skillB,
	}

	got, err := ResolveRequires(context.Background(), []PackageManifest{skillA, skillB}, catalog.lookup)
	if err != nil {
		t.Fatalf("ResolveRequires() error = %v", err)
	}

	want := []string{
		"mcp-server:office@1.0.0",
		"runtime:playwright-browsers@2.0.0",
		"skill:office-docx@1.4.0",
		"skill:office-xlsx@1.0.0",
	}
	gotKeys := keysOf(got)
	if len(gotKeys) != len(want) {
		t.Fatalf("ResolveRequires() = %v, want %v", gotKeys, want)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			t.Fatalf("ResolveRequires() = %v, want %v", gotKeys, want)
		}
	}
}

func TestResolveRequires_CycleDetection(t *testing.T) {
	a := mustManifest(t, PackageTypeSkill, "a", "1.0.0", "*", "skill:b@1.0.0")
	b := mustManifest(t, PackageTypeSkill, "b", "1.0.0", "*", "skill:a@1.0.0")
	catalog := fakeCatalog{
		"a@1.0.0": a,
		"b@1.0.0": b,
	}

	_, err := ResolveRequires(context.Background(), []PackageManifest{a}, catalog.lookup)
	if err == nil {
		t.Fatal("ResolveRequires() expected cycle error, got nil")
	}
	if !errors.Is(err, ErrRequiresCycle) {
		t.Fatalf("ResolveRequires() error = %v, want wrapping ErrRequiresCycle", err)
	}
}

func TestResolveRequires_SelfCycle(t *testing.T) {
	a := mustManifest(t, PackageTypeSkill, "a", "1.0.0", "*", "skill:a@1.0.0")
	catalog := fakeCatalog{"a@1.0.0": a}

	_, err := ResolveRequires(context.Background(), []PackageManifest{a}, catalog.lookup)
	if !errors.Is(err, ErrRequiresCycle) {
		t.Fatalf("ResolveRequires() error = %v, want ErrRequiresCycle", err)
	}
}

func TestResolveRequires_MissingDependency(t *testing.T) {
	skill := mustManifest(t, PackageTypeSkill, "office-docx", "1.4.0", "*", "runtime:playwright-browsers@2.0.0")
	catalog := fakeCatalog{"office-docx@1.4.0": skill}

	_, err := ResolveRequires(context.Background(), []PackageManifest{skill}, catalog.lookup)
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("ResolveRequires() error = %v, want wrapping ErrMissingDependency", err)
	}
}

func TestResolveRequires_TypeMismatch(t *testing.T) {
	wrongType := mustManifest(t, PackageTypeSkill, "office", "1.0.0", "*")
	skill := mustManifest(t, PackageTypeSkill, "office-docx", "1.4.0", "*", "runtime:office@1.0.0")
	catalog := fakeCatalog{
		"office@1.0.0":      wrongType,
		"office-docx@1.4.0": skill,
	}

	_, err := ResolveRequires(context.Background(), []PackageManifest{skill}, catalog.lookup)
	if err == nil {
		t.Fatal("ResolveRequires() expected type-mismatch error, got nil")
	}
}

func TestResolveRequires_NoRequires(t *testing.T) {
	skill := mustManifest(t, PackageTypeSkill, "solo", "1.0.0", "*")
	got, err := ResolveRequires(context.Background(), []PackageManifest{skill}, fakeCatalog{}.lookup)
	if err != nil {
		t.Fatalf("ResolveRequires() error = %v", err)
	}
	if len(got) != 1 || got[0].RequireKey() != skill.RequireKey() {
		t.Fatalf("ResolveRequires() = %+v, want [%+v]", got, skill)
	}
}

func TestResolveRequiresPartial_MissingDependencyExcludesChainOnly(t *testing.T) {
	ewsMCP := mustManifest(t, PackageTypeMCPServer, "ews-mcp", "0.1.0", "linux-x64")
	mailTriage := mustManifest(t, PackageTypeSkill, "mail-triage", "1.0.0", "*", "mcp-server:ews-mcp@0.1.0")
	officeMCP := mustManifest(t, PackageTypeMCPServer, "office", "1.0.0", "linux-x64")
	officeDocx := mustManifest(t, PackageTypeSkill, "office-docx", "1.0.0", "*", "mcp-server:office@1.0.0")

	catalog := fakeCatalog{

		"mail-triage@1.0.0": mailTriage,
		"office@1.0.0":      officeMCP,
		"office-docx@1.0.0": officeDocx,
	}

	resolved, unavailable, err := ResolveRequiresPartial(context.Background(), []PackageManifest{mailTriage, officeDocx}, catalog.lookup)
	if err != nil {
		t.Fatalf("ResolveRequiresPartial() error = %v", err)
	}

	wantResolved := []string{"mcp-server:office@1.0.0", "skill:office-docx@1.0.0"}
	if gotKeys := keysOf(resolved); len(gotKeys) != len(wantResolved) || gotKeys[0] != wantResolved[0] || gotKeys[1] != wantResolved[1] {
		t.Fatalf("ResolveRequiresPartial() resolved = %v, want %v", keysOf(resolved), wantResolved)
	}

	if len(unavailable) != 1 {
		t.Fatalf("ResolveRequiresPartial() unavailable = %+v, want 1 entry", unavailable)
	}
	if unavailable[0].Key != "skill:mail-triage@1.0.0" {
		t.Fatalf("ResolveRequiresPartial() unavailable[0].Key = %q, want mail-triage root key", unavailable[0].Key)
	}
	wantReason := "dependency unavailable: mail-triage requires ews-mcp@0.1.0"
	if unavailable[0].Reason != wantReason {
		t.Fatalf("ResolveRequiresPartial() unavailable[0].Reason = %q, want %q", unavailable[0].Reason, wantReason)
	}
	_ = ewsMCP
}

func TestResolveRequiresPartial_MultipleIndependentMissingRoots(t *testing.T) {
	other := mustManifest(t, PackageTypeSkill, "other", "1.0.0", "*")
	solo := mustManifest(t, PackageTypeSkill, "solo", "1.0.0", "*", "skill:missing@1.0.0")
	catalog := fakeCatalog{"other@1.0.0": other}

	resolved, unavailable, err := ResolveRequiresPartial(context.Background(), []PackageManifest{solo, other}, catalog.lookup)
	if err != nil {
		t.Fatalf("ResolveRequiresPartial() error = %v", err)
	}
	if len(resolved) != 1 || resolved[0].RequireKey() != "skill:other@1.0.0" {
		t.Fatalf("ResolveRequiresPartial() resolved = %v, want only skill:other@1.0.0", keysOf(resolved))
	}
	if len(unavailable) != 1 || unavailable[0].Key != "skill:solo@1.0.0" {
		t.Fatalf("ResolveRequiresPartial() unavailable = %+v, want solo excluded", unavailable)
	}
}

func TestResolveRequiresPartial_CycleStillHardFails(t *testing.T) {
	a := mustManifest(t, PackageTypeSkill, "a", "1.0.0", "*", "skill:b@1.0.0")
	b := mustManifest(t, PackageTypeSkill, "b", "1.0.0", "*", "skill:a@1.0.0")
	catalog := fakeCatalog{"a@1.0.0": a, "b@1.0.0": b}

	_, _, err := ResolveRequiresPartial(context.Background(), []PackageManifest{a}, catalog.lookup)
	if !errors.Is(err, ErrRequiresCycle) {
		t.Fatalf("ResolveRequiresPartial() error = %v, want ErrRequiresCycle", err)
	}
}

func TestResolveRequiresPartial_SharedDependencyResolvedOnce(t *testing.T) {
	runtime := mustManifest(t, PackageTypeRuntime, "playwright-browsers", "2.0.0", "*")
	skillA := mustManifest(t, PackageTypeSkill, "office-docx", "1.4.0", "*", "runtime:playwright-browsers@2.0.0")
	skillB := mustManifest(t, PackageTypeSkill, "office-xlsx", "1.0.0", "*", "runtime:playwright-browsers@2.0.0")
	catalog := fakeCatalog{
		"playwright-browsers@2.0.0": runtime,
		"office-docx@1.4.0":         skillA,
		"office-xlsx@1.0.0":         skillB,
	}

	resolved, unavailable, err := ResolveRequiresPartial(context.Background(), []PackageManifest{skillA, skillB}, catalog.lookup)
	if err != nil {
		t.Fatalf("ResolveRequiresPartial() error = %v", err)
	}
	if len(unavailable) != 0 {
		t.Fatalf("ResolveRequiresPartial() unavailable = %+v, want none", unavailable)
	}
	want := []string{"runtime:playwright-browsers@2.0.0", "skill:office-docx@1.4.0", "skill:office-xlsx@1.0.0"}
	if got := keysOf(resolved); len(got) != len(want) {
		t.Fatalf("ResolveRequiresPartial() resolved = %v, want %v", got, want)
	}
}

func TestFilterByPlatform(t *testing.T) {
	star := mustManifest(t, PackageTypeSkill, "agnostic", "1.0.0", "*")
	darwin := mustManifest(t, PackageTypeSkill, "mac-only", "1.0.0", "darwin-arm64")
	linux := mustManifest(t, PackageTypeSkill, "linux-only", "1.0.0", "linux-x64")

	all := []PackageManifest{star, darwin, linux}

	gotLinux := FilterByPlatform(all, "linux-x64")
	if len(gotLinux) != 2 {
		t.Fatalf("FilterByPlatform(linux-x64) = %v, want 2 entries (star + linux)", keysOf(gotLinux))
	}
	for _, m := range gotLinux {
		if m.Name == "mac-only" {
			t.Fatalf("FilterByPlatform(linux-x64) leaked a darwin-only package: %+v", m)
		}
	}

	gotDarwin := FilterByPlatform(all, "darwin-arm64")
	for _, m := range gotDarwin {
		if m.Name == "linux-only" {
			t.Fatalf("FilterByPlatform(darwin-arm64) leaked a linux-only package: %+v", m)
		}
	}
}
