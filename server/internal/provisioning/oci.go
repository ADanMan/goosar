package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	ociManifestLayerMediaType = "application/vnd.hermes.provisioning.manifest.v1+json"
	ociBlobLayerMediaType     = "application/vnd.hermes.provisioning.blob.v1.tar+zstd"
	ociManifestAcceptHeader   = "application/vnd.oci.image.manifest.v1+json"
)

type ociDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type ociManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Layers        []ociDescriptor `json:"layers"`
}

func (m ociManifest) layer(mediaType string) (ociDescriptor, bool) {
	for _, l := range m.Layers {
		if l.MediaType == mediaType {
			return l, true
		}
	}
	return ociDescriptor{}, false
}

type ociTagList struct {
	Tags []string `json:"tags"`
}

type ociCatalog struct {
	Repositories []string `json:"repositories"`
}

type OCIPackageStore struct {
	BaseURL string

	Repository string

	Username string
	Password string

	HTTPClient *http.Client

	token registryToken
}

func NewOCIPackageStoreFromEnv() *OCIPackageStore {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_OCI_URL")), "/")
	if base == "" {
		return nil
	}
	return &OCIPackageStore{
		BaseURL:    base,
		Repository: strings.Trim(strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_OCI_REPOSITORY")), "/"),
		Username:   os.Getenv("GOOSAR_PROVISIONING_OCI_USERNAME"),
		Password:   os.Getenv("GOOSAR_PROVISIONING_OCI_PASSWORD"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *OCIPackageStore) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *OCIPackageStore) repoPath(name string) string {
	if s.Repository == "" {
		return name
	}
	return s.Repository + "/" + name
}

func (s *OCIPackageStore) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if s.Username != "" || s.Password != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}
	return req, nil
}

func (s *OCIPackageStore) fetchOCIManifest(ctx context.Context, name, version string) (ociManifest, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", s.BaseURL, s.repoPath(name), url.PathEscape(version))
	resp, err := s.doAuthed(ctx, func() (*http.Request, error) {
		req, err := s.newRequest(ctx, http.MethodGet, manifestURL)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", ociManifestAcceptHeader)
		return req, nil
	})
	if err != nil {
		return ociManifest{}, fmt.Errorf("provisioning: failed to reach registry for %s@%s: %w", name, version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ociManifest{}, fmt.Errorf("%w: %s@%s", ErrPackageNotFound, name, version)
	}
	if resp.StatusCode != http.StatusOK {
		return ociManifest{}, fmt.Errorf("provisioning: registry returned status %d for %s@%s manifest", resp.StatusCode, name, version)
	}

	var om ociManifest
	if err := json.NewDecoder(resp.Body).Decode(&om); err != nil {
		return ociManifest{}, fmt.Errorf("%w: OCI manifest for %s@%s: %v", ErrMalformedManifest, name, version, err)
	}
	return om, nil
}

func (s *OCIPackageStore) fetchBlob(ctx context.Context, name, digest string) (io.ReadCloser, int64, error) {
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", s.BaseURL, s.repoPath(name), url.PathEscape(digest))
	resp, err := s.doAuthed(ctx, func() (*http.Request, error) {
		return s.newRequest(ctx, http.MethodGet, blobURL)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("provisioning: failed to reach registry for blob %s (%s): %w", digest, name, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("%w: blob %s for %s", ErrPackageNotFound, digest, name)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("provisioning: registry returned status %d for blob %s", resp.StatusCode, digest)
	}
	return resp.Body, resp.ContentLength, nil
}

func (s *OCIPackageStore) Manifest(ctx context.Context, name, version string) (PackageManifest, error) {
	om, err := s.fetchOCIManifest(ctx, name, version)
	if err != nil {
		return PackageManifest{}, err
	}
	layer, ok := om.layer(ociManifestLayerMediaType)
	if !ok {
		return PackageManifest{}, fmt.Errorf("%w: OCI manifest for %s@%s has no provisioning-manifest layer", ErrMalformedManifest, name, version)
	}
	reader, _, err := s.fetchBlob(ctx, name, layer.Digest)
	if err != nil {
		return PackageManifest{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return PackageManifest{}, err
	}
	m, err := ParsePackageManifest(data)
	if err != nil {
		return PackageManifest{}, fmt.Errorf("%w: %s@%s: %v", ErrMalformedManifest, name, version, err)
	}
	if m.Name != name || m.Version != version {
		return PackageManifest{}, fmt.Errorf("%w: OCI manifest at %s@%s claims identity %s@%s",
			ErrMalformedManifest, name, version, m.Name, m.Version)
	}
	return m, nil
}

func (s *OCIPackageStore) Blob(ctx context.Context, name, version string) (io.ReadCloser, int64, error) {
	om, err := s.fetchOCIManifest(ctx, name, version)
	if err != nil {
		return nil, 0, err
	}
	layer, ok := om.layer(ociBlobLayerMediaType)
	if !ok {
		return nil, 0, fmt.Errorf("%w: OCI manifest for %s@%s has no blob layer", ErrMalformedManifest, name, version)
	}
	reader, contentLength, err := s.fetchBlob(ctx, name, layer.Digest)
	if err != nil {
		return nil, 0, err
	}
	size := layer.Size
	if size <= 0 {
		size = contentLength
	}
	return reader, size, nil
}

func (s *OCIPackageStore) List(ctx context.Context) ([]PackageManifest, error) {
	catalogURL := fmt.Sprintf("%s/v2/_catalog", s.BaseURL)
	resp, err := s.doAuthed(ctx, func() (*http.Request, error) {
		return s.newRequest(ctx, http.MethodGet, catalogURL)
	})
	if err != nil {
		return nil, fmt.Errorf("provisioning: failed to list registry catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provisioning: registry returned status %d for catalog", resp.StatusCode)
	}
	var catalog ociCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("%w: registry catalog: %v", ErrMalformedManifest, err)
	}

	prefix := s.Repository
	out := make([]PackageManifest, 0)
	for _, repo := range catalog.Repositories {
		name, ok := packageNameFromRepo(repo, prefix)
		if !ok {
			continue
		}

		tagsURL := fmt.Sprintf("%s/v2/%s/tags/list", s.BaseURL, repo)
		tagsResp, err := s.doAuthed(ctx, func() (*http.Request, error) {
			return s.newRequest(ctx, http.MethodGet, tagsURL)
		})
		if err != nil {
			return nil, fmt.Errorf("provisioning: failed to list tags for %s: %w", name, err)
		}
		var tagList ociTagList
		decodeErr := json.NewDecoder(tagsResp.Body).Decode(&tagList)
		tagsResp.Body.Close()
		if tagsResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("provisioning: registry returned status %d listing tags for %s", tagsResp.StatusCode, name)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("%w: tags list for %s: %v", ErrMalformedManifest, name, decodeErr)
		}

		for _, tag := range tagList.Tags {
			m, err := s.Manifest(ctx, name, tag)
			if err != nil {
				return nil, fmt.Errorf("catalog entry %s@%s: %w", name, tag, err)
			}
			out = append(out, m)
		}
	}
	return out, nil
}

func packageNameFromRepo(repo, prefix string) (string, bool) {
	if prefix == "" {
		return repo, true
	}
	trimmed := strings.TrimPrefix(repo, prefix+"/")
	if trimmed == repo {
		return "", false
	}
	return trimmed, true
}
