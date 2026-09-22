package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selfAt(t *testing.T, dir string) *SelfInfo {
	t.Helper()
	path := fakeBin(t, dir, "goosar", "goosar version 0.11.0 (commit: abc, built: now)")
	return &SelfInfo{Path: path, Version: "0.11.0"}
}

func TestGoosarOnPath_IsTheFirstRow(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	report := Run(context.Background(), Options{Self: selfAt(t, dir)})
	if len(report.Results) == 0 || report.Results[0].ID != "goosar" {
		t.Fatalf("first row = %v, want goosar", ids(report))
	}
}

func TestGoosarOnPath_SameBinaryIsOK(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	row := findResult(t, Run(context.Background(), Options{Self: selfAt(t, dir)}), "goosar")
	if row.Status != StatusOK {
		t.Fatalf("goosar on PATH = %+v, want ok", row)
	}
	if !strings.Contains(row.Detail, dir) {
		t.Errorf("detail should name the resolved path, got %q", row.Detail)
	}
}

func TestGoosarOnPath_SymlinkToSelfIsOK(t *testing.T) {
	binDir := t.TempDir()
	appDir := t.TempDir()
	self := selfAt(t, appDir)
	if err := os.Symlink(self.Path, filepath.Join(binDir, "goosar")); err != nil {
		t.Fatal(err)
	}
	isolatedPath(t, binDir)
	row := findResult(t, Run(context.Background(), Options{Self: self}), "goosar")
	if row.Status != StatusOK {
		t.Fatalf("symlinked goosar = %+v, want ok (T-01 installs exactly this)", row)
	}
}

func TestGoosarOnPath_MissingPointsAtTheMenuItem(t *testing.T) {
	binDir := t.TempDir()
	appDir := t.TempDir()
	isolatedPath(t, binDir)
	row := findResult(t, Run(context.Background(), Options{Self: selfAt(t, appDir)}), "goosar")
	if row.Status != StatusMissing {
		t.Fatalf("goosar absent from PATH = %+v, want missing", row)
	}
	if !strings.Contains(row.Fix, "Установить командную строку") {
		t.Errorf("fix should name the app menu item, got %q", row.Fix)
	}
}

func TestGoosarOnPath_ForeignOlderBinaryIsOutdated(t *testing.T) {
	binDir := t.TempDir()
	appDir := t.TempDir()
	fakeBin(t, binDir, "goosar", "goosar version 0.10.1 (commit: old, built: then)")
	isolatedPath(t, binDir)
	row := findResult(t, Run(context.Background(), Options{Self: selfAt(t, appDir)}), "goosar")
	if row.Status != StatusOutdated {
		t.Fatalf("older foreign goosar = %+v, want outdated", row)
	}
	for _, v := range []string{"0.10.1", "0.11.0"} {
		if !strings.Contains(row.Message, v) {
			t.Errorf("message should carry both versions, missing %s: %q", v, row.Message)
		}
	}
}

func TestGoosarOnPath_ForeignSameVersionIsOK(t *testing.T) {
	binDir := t.TempDir()
	appDir := t.TempDir()
	fakeBin(t, binDir, "goosar", "goosar version 0.11.0 (commit: brew, built: now)")
	isolatedPath(t, binDir)
	row := findResult(t, Run(context.Background(), Options{Self: selfAt(t, appDir)}), "goosar")
	if row.Status != StatusOK {
		t.Fatalf("same-version foreign goosar = %+v, want ok", row)
	}
}

func TestGoosarOnPath_UnknownSelfIsSkipped(t *testing.T) {
	dir := t.TempDir()
	isolatedPath(t, dir)
	row := findResult(t, Run(context.Background(), Options{}), "goosar")
	if row.Status != StatusSkipped {
		t.Fatalf("without SelfInfo the row = %+v, want skipped (support bundle passes no self)", row)
	}
}
