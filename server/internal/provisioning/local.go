package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/adanman/goosar/server/internal/storage"
)

type LocalPackageStore struct {
	storage storage.Storage
	prefix  string
}

type catalogEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func NewLocalPackageStore(backend storage.Storage, prefix string) *LocalPackageStore {
	if strings.TrimSpace(prefix) == "" {
		prefix = "provisioning"
	}
	return &LocalPackageStore{storage: backend, prefix: strings.Trim(prefix, "/")}
}

func (s *LocalPackageStore) catalogKey() string {
	return path.Join(s.prefix, "catalog.json")
}

func (s *LocalPackageStore) manifestKey(name, version string) string {
	return path.Join(s.prefix, name, version, "manifest.json")
}

func (s *LocalPackageStore) readManifestBytes(ctx context.Context, name, version string) ([]byte, error) {
	reader, err := s.storage.GetReader(ctx, s.manifestKey(name, version))
	if err != nil {
		return nil, fmt.Errorf("%w: %s@%s: %v", ErrPackageNotFound, name, version, err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (s *LocalPackageStore) Manifest(ctx context.Context, name, version string) (PackageManifest, error) {
	data, err := s.readManifestBytes(ctx, name, version)
	if err != nil {
		return PackageManifest{}, err
	}
	m, err := ParsePackageManifest(data)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("%w: %s@%s: %v", ErrMalformedManifest, name, version, err)
	}
	if m.Name != name || m.Version != version {
		return PackageManifest{}, fmt.Errorf("%w: manifest at %s@%s claims identity %s@%s",
			ErrMalformedManifest, name, version, m.Name, m.Version)
	}
	return m, nil
}

func (s *LocalPackageStore) Blob(ctx context.Context, name, version string) (io.ReadCloser, int64, error) {
	m, err := s.Manifest(ctx, name, version)
	if err != nil {
		return nil, 0, err
	}
	key := path.Join(s.prefix, name, version, m.BlobFilename())
	reader, err := s.storage.GetReader(ctx, key)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: blob for %s@%s: %v", ErrPackageNotFound, name, version, err)
	}
	return reader, m.Size, nil
}

func (s *LocalPackageStore) List(ctx context.Context) ([]PackageManifest, error) {
	reader, err := s.storage.GetReader(ctx, s.catalogKey())
	if err != nil {

		if storage.IsObjectNotFound(err) {
			return nil, fmt.Errorf("%w: catalog: %v", ErrCatalogNotFound, err)
		}
		return nil, fmt.Errorf("provisioning: read catalog: %w", err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return nil, err
	}

	var entries []catalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%w: catalog: %v", ErrMalformedManifest, err)
	}

	out := make([]PackageManifest, 0, len(entries))
	for _, entry := range entries {
		m, err := s.Manifest(ctx, entry.Name, entry.Version)
		if err != nil {
			return nil, fmt.Errorf("catalog entry %s@%s: %w", entry.Name, entry.Version, err)
		}
		out = append(out, m)
	}
	return out, nil
}
