package provisioning

import (
	"context"
	"errors"
	"io"
)

type PackageStore interface {
	Manifest(ctx context.Context, name, version string) (PackageManifest, error)

	Blob(ctx context.Context, name, version string) (io.ReadCloser, int64, error)

	List(ctx context.Context) ([]PackageManifest, error)
}

var ErrPackageNotFound = errors.New("provisioning: package not found")

var ErrMalformedManifest = errors.New("provisioning: malformed manifest")

var ErrCatalogNotFound = errors.New("provisioning: catalog not found")
