package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adanman/goosar/server/internal/storage"
)

func newTestLocalStore(t *testing.T) (*LocalPackageStore, storage.Storage) {
	t.Helper()
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	backend := storage.NewLocalStorageFromEnv()
	if backend == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}
	return NewLocalPackageStore(backend, "provisioning"), backend
}

func blobBytes() []byte {
	return []byte("fake tar.zst content for office-docx 1.4.0")
}

func publishFixture(t *testing.T, backend storage.Storage, prefix string, m PackageManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal fixture manifest: %v", err)
	}
	if _, err := backend.Upload(context.Background(), prefix+"/"+m.Name+"/"+m.Version+"/manifest.json", data, "application/json", "manifest.json"); err != nil {
		t.Fatalf("upload fixture manifest: %v", err)
	}
	if _, err := backend.Upload(context.Background(), prefix+"/"+m.Name+"/"+m.Version+"/"+m.BlobFilename(), blobBytes(), "application/zstd", m.BlobFilename()); err != nil {
		t.Fatalf("upload fixture blob: %v", err)
	}
}

func publishCatalog(t *testing.T, backend storage.Storage, prefix string, entries []catalogEntry) {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal fixture catalog: %v", err)
	}
	if _, err := backend.Upload(context.Background(), prefix+"/catalog.json", data, "application/json", "catalog.json"); err != nil {
		t.Fatalf("upload fixture catalog: %v", err)
	}
}

func fixtureManifest() PackageManifest {
	sum := sha256.Sum256(blobBytes())
	return PackageManifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          "office-docx",
		Version:       "1.4.0",
		Type:          PackageTypeSkill,
		Platform:      "darwin-arm64",
		SHA256:        hex.EncodeToString(sum[:]),
		Size:          int64(len(blobBytes())),
	}
}

func TestLocalPackageStore_RoundTrip(t *testing.T) {
	store, backend := newTestLocalStore(t)
	fixture := fixtureManifest()
	publishFixture(t, backend, "provisioning", fixture)

	got, err := store.Manifest(context.Background(), fixture.Name, fixture.Version)
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}

	want := fixture
	if want.Requires == nil {
		want.Requires = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Manifest() = %+v, want %+v", got, want)
	}

	reader, size, err := store.Blob(context.Background(), fixture.Name, fixture.Version)
	if err != nil {
		t.Fatalf("Blob() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(data) != string(blobBytes()) {
		t.Fatalf("blob content mismatch: got %q, want %q", data, blobBytes())
	}
	if size != fixture.Size {
		t.Fatalf("Blob() size = %d, want %d", size, fixture.Size)
	}
}

func TestLocalPackageStore_ManifestNotFound(t *testing.T) {
	store, _ := newTestLocalStore(t)
	_, err := store.Manifest(context.Background(), "nonexistent", "1.0.0")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Manifest() error = %v, want ErrPackageNotFound", err)
	}
}

func TestLocalPackageStore_BlobNotFound(t *testing.T) {
	store, _ := newTestLocalStore(t)
	_, _, err := store.Blob(context.Background(), "nonexistent", "1.0.0")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Blob() error = %v, want ErrPackageNotFound", err)
	}
}

func TestLocalPackageStore_MalformedManifest(t *testing.T) {
	store, backend := newTestLocalStore(t)
	if _, err := backend.Upload(context.Background(), "provisioning/broken/1.0.0/manifest.json", []byte("{not valid json"), "application/json", "manifest.json"); err != nil {
		t.Fatalf("upload malformed manifest: %v", err)
	}

	_, err := store.Manifest(context.Background(), "broken", "1.0.0")
	if err == nil {
		t.Fatal("Manifest() expected error for malformed manifest.json, got nil")
	}
}

func TestLocalPackageStore_ManifestIdentityMismatch(t *testing.T) {
	store, backend := newTestLocalStore(t)
	mismatched := fixtureManifest()
	mismatched.Name = "different-name"
	data, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if _, err := backend.Upload(context.Background(), "provisioning/office-docx/1.4.0/manifest.json", data, "application/json", "manifest.json"); err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, err = store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() expected identity-mismatch error, got nil")
	}
}

func TestLocalPackageStore_List(t *testing.T) {
	store, backend := newTestLocalStore(t)
	a := fixtureManifest()
	b := fixtureManifest()
	b.Name = "office-xlsx"
	publishFixture(t, backend, "provisioning", a)
	publishFixture(t, backend, "provisioning", b)
	publishCatalog(t, backend, "provisioning", []catalogEntry{
		{Name: a.Name, Version: a.Version},
		{Name: b.Name, Version: b.Version},
	})

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d entries, want 2", len(got))
	}
}

func TestLocalPackageStore_List_MissingCatalog(t *testing.T) {
	store, _ := newTestLocalStore(t)
	_, err := store.List(context.Background())
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("List() error = %v, want ErrCatalogNotFound", err)
	}
	if errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("List() error = %v, should not also satisfy ErrPackageNotFound", err)
	}
}

func TestLocalPackageStore_List_MalformedCatalog(t *testing.T) {
	store, backend := newTestLocalStore(t)
	if _, err := backend.Upload(context.Background(), "provisioning/catalog.json", []byte("not json"), "application/json", "catalog.json"); err != nil {
		t.Fatalf("upload malformed catalog: %v", err)
	}
	if _, err := store.List(context.Background()); err == nil {
		t.Fatal("List() expected error for malformed catalog.json, got nil")
	}
}

func TestLocalPackageStore_List_DanglingCatalogEntry(t *testing.T) {
	store, backend := newTestLocalStore(t)
	publishCatalog(t, backend, "provisioning", []catalogEntry{{Name: "ghost", Version: "1.0.0"}})
	if _, err := store.List(context.Background()); err == nil {
		t.Fatal("List() expected error for dangling catalog entry, got nil")
	}
}

func TestLocalPackageStore_List_UnreadableCatalogIsNotNotFound(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 does not produce EACCES")
	}
	store, _ := newTestLocalStore(t)
	uploadDir := os.Getenv("LOCAL_UPLOAD_DIR")
	catalogPath := filepath.Join(uploadDir, "provisioning", "catalog.json")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(catalogPath, []byte("[]"), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	if err := os.Chmod(catalogPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(catalogPath, 0o600) })

	_, err := store.List(context.Background())
	if err == nil {
		t.Fatal("List() expected error for unreadable catalog.json, got nil")
	}
	if errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("List() error = %v, must NOT read as ErrCatalogNotFound (that is the mass-revocation path)", err)
	}
}
