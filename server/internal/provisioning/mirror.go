package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
)

type PackageRef struct {
	Name    string
	Version string
}

func ParsePackageRefs(raw string) ([]PackageRef, error) {
	var refs []PackageRef
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, version, ok := strings.Cut(part, "@")
		if !ok {
			return nil, fmt.Errorf("provisioning: package ref %q is not name@version", part)
		}
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		if !ValidPackageIdentifier(name) || !ValidPackageIdentifier(version) {
			return nil, fmt.Errorf("provisioning: package ref %q has an invalid name or version", part)
		}
		refs = append(refs, PackageRef{Name: name, Version: version})
	}
	return refs, nil
}

func MirrorToLocal(ctx context.Context, src PackageStore, dst *LocalPackageStore, refs []PackageRef) (int, error) {
	if src == nil || dst == nil {
		return 0, errors.New("provisioning: mirror needs both a source and a destination store")
	}

	if len(refs) == 0 {
		listed, err := src.List(ctx)
		if err != nil {
			return 0, fmt.Errorf("provisioning: enumerate source catalog: %w", err)
		}
		for _, m := range listed {
			refs = append(refs, PackageRef{Name: m.Name, Version: m.Version})
		}
	}
	if len(refs) == 0 {
		return 0, nil
	}

	copied := make([]PackageManifest, 0, len(refs))
	for _, ref := range refs {
		m, err := src.Manifest(ctx, ref.Name, ref.Version)
		if err != nil {
			return 0, fmt.Errorf("provisioning: source manifest %s@%s: %w", ref.Name, ref.Version, err)
		}
		blob, err := readBlob(ctx, src, ref)
		if err != nil {
			return 0, err
		}
		if int64(len(blob)) != m.Size {
			return 0, fmt.Errorf("provisioning: %s@%s blob size %d does not match its manifest size %d — refusing a corrupt package",
				ref.Name, ref.Version, len(blob), m.Size)
		}
		sum := sha256.Sum256(blob)
		if actual := hex.EncodeToString(sum[:]); actual != m.SHA256 {
			return 0, fmt.Errorf("provisioning: %s@%s blob sha256 %s does not match its manifest sha256 %s — refusing a corrupt package",
				ref.Name, ref.Version, actual, m.SHA256)
		}
		if err := dst.PutPackage(ctx, m, blob); err != nil {
			return 0, err
		}
		copied = append(copied, m)
	}

	if err := dst.PutCatalog(ctx, copied); err != nil {
		return 0, err
	}
	return len(copied), nil
}

func readBlob(ctx context.Context, src PackageStore, ref PackageRef) ([]byte, error) {
	reader, _, err := src.Blob(ctx, ref.Name, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("provisioning: source blob %s@%s: %w", ref.Name, ref.Version, err)
	}
	defer reader.Close()
	blob, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read source blob %s@%s: %w", ref.Name, ref.Version, err)
	}
	return blob, nil
}

func (s *LocalPackageStore) PutPackage(ctx context.Context, m PackageManifest, blob []byte) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("provisioning: marshal manifest %s@%s: %w", m.Name, m.Version, err)
	}
	blobKey := path.Join(s.prefix, m.Name, m.Version, m.BlobFilename())
	if _, err := s.storage.Upload(ctx, blobKey, blob, "application/zstd", m.BlobFilename()); err != nil {
		return fmt.Errorf("provisioning: write blob %s@%s: %w", m.Name, m.Version, err)
	}
	if _, err := s.storage.Upload(ctx, s.manifestKey(m.Name, m.Version), data, "application/json", "manifest.json"); err != nil {
		return fmt.Errorf("provisioning: write manifest %s@%s: %w", m.Name, m.Version, err)
	}
	return nil
}

func (s *LocalPackageStore) PutCatalog(ctx context.Context, manifests []PackageManifest) error {
	entries := make([]catalogEntry, 0, len(manifests))
	for _, m := range manifests {
		entries = append(entries, catalogEntry{Name: m.Name, Version: m.Version})
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("provisioning: marshal catalog: %w", err)
	}
	if _, err := s.storage.Upload(ctx, s.catalogKey(), data, "application/json", "catalog.json"); err != nil {
		return fmt.Errorf("provisioning: write catalog: %w", err)
	}
	return nil
}

const KitMissingHint = "provisioning packages are missing: run `make selfhost-packages INDEX_URL=<url>`, " +
	"or set GOOSAR_PROVISIONING_OCI_URL to an in-perimeter registry, " +
	"or load a package index with `bash offline/load-provisioning-packages.sh --index-url <url>`"

func EnsureLocalKit(ctx context.Context, src PackageStore, dst *LocalPackageStore, refs []PackageRef, log *slog.Logger) PackageStore {
	if log == nil {
		log = slog.Default()
	}
	if dst == nil {
		if src == nil {
			log.Warn(KitMissingHint)
		}
		return src
	}

	existing, err := dst.List(ctx)
	switch {
	case err == nil && len(existing) > 0:
		log.Info("provisioning: local kit already present, skipping first-boot sync", "packages", len(existing))
		return dst
	case err != nil && !errors.Is(err, ErrCatalogNotFound):
		log.Error("provisioning: local kit unreadable, falling back to the configured source store", "error", err)
		if src != nil {
			return src
		}
		return dst
	}

	if src == nil {
		log.Warn(KitMissingHint)
		return dst
	}

	log.Info("provisioning: no local kit yet — syncing from the configured registry on first boot")
	n, err := MirrorToLocal(ctx, src, dst, refs)
	if err != nil {
		log.Error("provisioning: first-boot kit sync failed, serving from the registry directly", "error", err, "hint", KitMissingHint)
		return src
	}
	log.Info("provisioning: first-boot kit sync complete", "packages", n)
	return dst
}
