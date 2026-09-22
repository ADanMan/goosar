package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	uploadDir string
	baseURL   string
}

const metaSuffix = ".meta.json"

const tempSuffix = ".tmp"

const ExportsSubdir = "exports"

type localMeta struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

func NewLocalStorageFromEnv() *LocalStorage {
	uploadDir := os.Getenv("LOCAL_UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./data/uploads"
	}

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		slog.Error("failed to create upload directory", "dir", uploadDir, "error", err)
		return nil
	}

	baseURL := strings.TrimSuffix(os.Getenv("LOCAL_UPLOAD_BASE_URL"), "/")

	slog.Info("local storage initialized", "dir", uploadDir, "baseURL", baseURL)
	return &LocalStorage{
		uploadDir: uploadDir,
		baseURL:   baseURL,
	}
}

func (s *LocalStorage) CdnDomain() string {
	if s.baseURL == "" {
		return ""
	}
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *LocalStorage) KeyFromURL(rawURL string) string {
	if s.baseURL != "" && strings.HasPrefix(rawURL, s.baseURL) {
		rawURL = strings.TrimPrefix(rawURL, s.baseURL)
	}

	prefix := "/uploads/"
	if idx := strings.Index(rawURL, prefix); idx >= 0 {
		return rawURL[idx+len(prefix):]
	}
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func (s *LocalStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, fmt.Errorf("local GetReader: empty key")
	}
	if isInternalLocalPath(key) {
		return nil, fmt.Errorf("local GetReader: refusing to serve internal key %q", key)
	}
	filePath := filepath.Join(s.uploadDir, key)
	if !isUnder(s.uploadDir, filePath) {
		return nil, fmt.Errorf("local GetReader: key escapes upload dir: %q", key)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("local GetReader: %w", err)
	}
	return f, nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) {
	if err := s.DeleteObject(ctx, key); err != nil {
		slog.Error("local storage Delete failed", "key", key, "error", err)
	}
}

func (s *LocalStorage) DeleteObject(_ context.Context, key string) error {
	if key == "" {
		return nil
	}
	filePath := filepath.Join(s.uploadDir, key)
	for _, p := range []string{filePath, filePath + metaSuffix, tempPath(filePath)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *LocalStorage) ObjectURL(key string) string {
	if s.baseURL != "" {
		return fmt.Sprintf("%s/uploads/%s", s.baseURL, key)
	}
	return fmt.Sprintf("/uploads/%s", key)
}

func (s *LocalStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *LocalStorage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	dest := filepath.Join(s.uploadDir, key)
	if err := writeAtomic(dest, bytes.NewReader(data)); err != nil {
		return "", err
	}

	if filename != "" {
		body, _ := json.Marshal(localMeta{Filename: filename, ContentType: contentType})
		if err := os.WriteFile(dest+metaSuffix, body, 0o644); err != nil {
			slog.Error("local storage meta write failed", "key", key, "error", err)
		}
	}

	if s.baseURL != "" {
		return fmt.Sprintf("%s/uploads/%s", s.baseURL, key), nil
	}
	return fmt.Sprintf("/uploads/%s", key), nil
}

func (s *LocalStorage) UploadStream(ctx context.Context, key string, data io.Reader, _ int64, contentType string, filename string) (string, error) {
	dest := filepath.Join(s.uploadDir, key)
	if err := writeAtomic(dest, data); err != nil {
		return "", err
	}
	if filename != "" {
		body, _ := json.Marshal(localMeta{Filename: filename, ContentType: contentType})
		if err := os.WriteFile(dest+metaSuffix, body, 0o644); err != nil {
			slog.Error("local storage meta write failed", "key", key, "error", err)
		}
	}

	if s.baseURL != "" {
		return fmt.Sprintf("%s/uploads/%s", s.baseURL, key), nil
	}
	return fmt.Sprintf("/uploads/%s", key), nil
}

func writeAtomic(dest string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("local storage MkdirAll: %w", err)
	}
	tmp := tempPath(dest)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("local storage create temp: %w", err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("local storage stream copy: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("local storage Close: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("local storage Rename: %w", err)
	}
	return nil
}

func tempPath(dest string) string {
	return filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+tempSuffix)
}

func isInternalLocalPath(key string) bool {
	if strings.HasSuffix(key, metaSuffix) {
		return true
	}
	if isExportPath(key) {
		return true
	}
	base := filepath.Base(key)
	return strings.HasPrefix(base, ".") && strings.HasSuffix(base, tempSuffix)
}

func isExportPath(key string) bool {
	cleaned := filepath.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	return cleaned == "/"+ExportsSubdir || strings.HasPrefix(cleaned, "/"+ExportsSubdir+"/")
}

func (s *LocalStorage) GetFilePath(key string) string {
	return filepath.Join(s.uploadDir, key)
}

func (s *LocalStorage) ServeFile(w http.ResponseWriter, r *http.Request, filename string) {

	if isInternalLocalPath(filename) {
		http.NotFound(w, r)
		return
	}

	filePath := filepath.Join(s.uploadDir, filename)

	if !isUnder(s.uploadDir, filePath) {
		http.NotFound(w, r)
		return
	}
	slog.Info("serving file", "filename", filename, "filepath", filePath)

	if meta, ok := readLocalMeta(filePath); ok && meta.Filename != "" {
		w.Header().Set("Content-Disposition", ContentDisposition(meta.ContentType, meta.Filename))
	}

	http.ServeFile(w, r, filePath)
}

func isUnder(dir, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readLocalMeta(filePath string) (localMeta, bool) {
	body, err := os.ReadFile(filePath + metaSuffix)
	if err != nil {
		return localMeta{}, false
	}
	var meta localMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return localMeta{}, false
	}
	return meta, true
}

func (s *LocalStorage) UploadFromReader(ctx context.Context, key string, reader io.Reader, contentType string, filename string) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("local storage ReadAll: %w", err)
	}

	return s.Upload(ctx, key, data, contentType, filename)
}
