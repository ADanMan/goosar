package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSymlink_SkipsWhenSourceMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "missing.json")
	dst := filepath.Join(dir, "link.json")

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink: %v", err)
	}

	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("expected dst to not be created when src is missing")
	}
}

func TestEnsureSymlink_ReplacesStaleRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "existing.json")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("dst content = %q, want %q (file should be re-linked/re-copied from src)", data, "new")
	}
}

func TestEnsureSymlink_RefreshesAfterCopyFallbackThenSrcChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "auth.json")
	dst := filepath.Join(dir, "task-auth.json")

	os.WriteFile(src, []byte(`{"refresh_token":"v1"}`), 0o644)
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("seed copy fallback: %v", err)
	}

	os.WriteFile(src, []byte(`{"refresh_token":"v2"}`), 0o644)

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != `{"refresh_token":"v2"}` {
		t.Errorf("dst content after refresh = %q, want v2 contents", data)
	}
}

func TestCreateDirLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "test.txt"), []byte("hello"), 0o644)

	if err := createDirLink(src, dst); err != nil {
		t.Fatalf("createDirLink: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dst, "test.txt"))
	if err != nil {
		t.Fatalf("read through link: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", data, "hello")
	}
}

func TestCreateFileLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "link.json")
	os.WriteFile(src, []byte(`{"key":"value"}`), 0o644)

	if err := createFileLink(src, dst); err != nil {
		t.Fatalf("createFileLink: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	if string(data) != `{"key":"value"}` {
		t.Errorf("content = %q", data)
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("content"), 0o644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, _ := os.ReadFile(dst)
	if string(data) != "content" {
		t.Errorf("content = %q", data)
	}

	fi, _ := os.Lstat(dst)
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("expected regular file, not symlink")
	}
}
