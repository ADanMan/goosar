package asset

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestLocalStorageSaveOpenDelete(t *testing.T) {
	st := NewLocalStorage(t.TempDir())

	stored, err := st.Save(t.Context(), "workspaces/ws1", "report.txt", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if stored.Key == "" || stored.DownloadPath != "/uploads/"+stored.Key {
		t.Errorf("unexpected StoredObject: %+v", stored)
	}

	r, size, err := st.Open(t.Context(), stored.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if size != int64(len("hello world")) {
		t.Errorf("size = %d, want %d", size, len("hello world"))
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read: %v", err)
	}
	if buf.String() != "hello world" {
		t.Errorf("content = %q, want %q", buf.String(), "hello world")
	}

	if err := st.Delete(t.Context(), stored.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := st.Open(t.Context(), stored.Key); err == nil {
		t.Error("Open after Delete should fail")
	}
	// Повторный Delete — не ошибка (идемпотентно).
	if err := st.Delete(t.Context(), stored.Key); err != nil {
		t.Errorf("second Delete should be a no-op, got: %v", err)
	}
}

func TestLocalStorageRejectsEscapingKeys(t *testing.T) {
	st := NewLocalStorage(t.TempDir())
	if _, _, err := st.Open(t.Context(), "../../etc/passwd"); err == nil {
		t.Error("Open with a path-escaping key должен быть отклонён")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"report.txt":       "report.txt",
		"../../etc/passwd": "passwd",
		"":                 "file",
		"  spaced.pdf  ":   "spaced.pdf",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTextLike(t *testing.T) {
	textLike := []string{"text/plain", "text/plain; charset=utf-8", "application/json", "application/x-yaml"}
	for _, ct := range textLike {
		if !isTextLike(ct) {
			t.Errorf("isTextLike(%q) = false, want true", ct)
		}
	}
	notTextLike := []string{"image/png", "application/pdf", "application/octet-stream"}
	for _, ct := range notTextLike {
		if isTextLike(ct) {
			t.Errorf("isTextLike(%q) = true, want false", ct)
		}
	}
}

func TestSniffContentType(t *testing.T) {
	ct, body, err := sniffContentType(strings.NewReader("hello"), "readme.txt")
	if err != nil {
		t.Fatalf("sniffContentType: %v", err)
	}
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type = %q, want text/plain by extension", ct)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, body); err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if buf.String() != "hello" {
		t.Errorf("body content lost during sniff: got %q", buf.String())
	}
}

func TestS3StorageNotImplemented(t *testing.T) {
	st := NewS3Storage()
	if _, err := st.Save(t.Context(), "scope", "f.txt", strings.NewReader("x")); err != ErrStorageNotImplemented {
		t.Errorf("Save error = %v, want ErrStorageNotImplemented", err)
	}
	if _, _, err := st.Open(t.Context(), "key"); err != ErrStorageNotImplemented {
		t.Errorf("Open error = %v, want ErrStorageNotImplemented", err)
	}
	if err := st.Delete(t.Context(), "key"); err != ErrStorageNotImplemented {
		t.Errorf("Delete error = %v, want ErrStorageNotImplemented", err)
	}
}
