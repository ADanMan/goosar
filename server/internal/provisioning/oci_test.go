package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubRegistry struct {
	manifests map[string]ociManifest
	blobs     map[string][]byte
	tags      map[string][]string
	repos     []string

	rawManifestOverride map[string][]byte

	rawBlobOverride map[string][]byte
}

func newStubRegistry() *stubRegistry {
	return &stubRegistry{
		manifests:           map[string]ociManifest{},
		blobs:               map[string][]byte{},
		tags:                map[string][]string{},
		rawManifestOverride: map[string][]byte{},
		rawBlobOverride:     map[string][]byte{},
	}
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *stubRegistry) publish(repo, tag string, manifestJSON, blobBytes []byte) {
	manifestDigest := digestOf(manifestJSON)
	blobDigest := digestOf(blobBytes)
	r.blobs[repo+"/"+manifestDigest] = manifestJSON
	r.blobs[repo+"/"+blobDigest] = blobBytes
	r.manifests[repo+"/"+tag] = ociManifest{
		SchemaVersion: 2,
		Layers: []ociDescriptor{
			{MediaType: ociManifestLayerMediaType, Digest: manifestDigest, Size: int64(len(manifestJSON))},
			{MediaType: ociBlobLayerMediaType, Digest: blobDigest, Size: int64(len(blobBytes))},
		},
	}
	found := false
	for _, existing := range r.tags[repo] {
		if existing == tag {
			found = true
		}
	}
	if !found {
		r.tags[repo] = append(r.tags[repo], tag)
	}
	repoFound := false
	for _, existing := range r.repos {
		if existing == repo {
			repoFound = true
		}
	}
	if !repoFound {
		r.repos = append(r.repos, repo)
	}
}

func (r *stubRegistry) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"repositories": r.repos})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path

		if idx := indexOfSubstr(path, "/manifests/"); idx >= 0 {
			repo := trimPrefixSlash(path[len("/v2/"):idx])
			ref := path[idx+len("/manifests/"):]
			key := repo + "/" + ref
			if raw, ok := r.rawManifestOverride[key]; ok {
				w.Write(raw)
				return
			}
			om, ok := r.manifests[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(om)
			return
		}

		if idx := indexOfSubstr(path, "/tags/list"); idx >= 0 {
			repo := trimPrefixSlash(path[len("/v2/"):idx])
			json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": r.tags[repo]})
			return
		}

		if idx := indexOfSubstr(path, "/blobs/"); idx >= 0 {
			repo := trimPrefixSlash(path[len("/v2/"):idx])
			digest := path[idx+len("/blobs/"):]
			key := repo + "/" + digest
			if raw, ok := r.rawBlobOverride[key]; ok {
				w.Write(raw)
				return
			}
			data, ok := r.blobs[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func indexOfSubstr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimPrefixSlash(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func ociFixtureManifest() PackageManifest {
	blob := []byte("fake oci tar.zst content")
	sum := sha256.Sum256(blob)
	return PackageManifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          "office-docx",
		Version:       "1.4.0",
		Type:          PackageTypeSkill,
		Platform:      "linux-x64",
		SHA256:        hex.EncodeToString(sum[:]),
		Size:          int64(len(blob)),
	}
}

func TestOCIPackageStore_RoundTrip(t *testing.T) {
	reg := newStubRegistry()
	m := ociFixtureManifest()
	manifestJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	blobBytes := []byte("fake oci tar.zst content")
	reg.publish("hermes/provisioning/office-docx", "1.4.0", manifestJSON, blobBytes)
	srv := reg.server(t)

	store := &OCIPackageStore{
		BaseURL:    srv.URL,
		Repository: "hermes/provisioning",
		HTTPClient: srv.Client(),
	}

	got, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	if got.Name != m.Name || got.Version != m.Version || got.SHA256 != m.SHA256 {
		t.Fatalf("Manifest() = %+v, want %+v", got, m)
	}

	reader, size, err := store.Blob(context.Background(), "office-docx", "1.4.0")
	if err != nil {
		t.Fatalf("Blob() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(data) != string(blobBytes) {
		t.Fatalf("blob mismatch: got %q want %q", data, blobBytes)
	}
	if size != m.Size {
		t.Fatalf("Blob() size = %d, want %d", size, m.Size)
	}
}

func TestOCIPackageStore_ManifestNotFound(t *testing.T) {
	reg := newStubRegistry()
	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	_, err := store.Manifest(context.Background(), "nonexistent", "1.0.0")
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Manifest() error = %v, want ErrPackageNotFound", err)
	}
}

func TestOCIPackageStore_MalformedOCIManifest(t *testing.T) {
	reg := newStubRegistry()
	reg.rawManifestOverride["hermes/provisioning/office-docx/1.4.0"] = []byte("not json")
	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	_, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() expected error for malformed OCI manifest, got nil")
	}
}

func TestOCIPackageStore_MissingManifestLayer(t *testing.T) {
	reg := newStubRegistry()
	reg.manifests["hermes/provisioning/office-docx/1.4.0"] = ociManifest{SchemaVersion: 2}
	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	_, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() expected error for missing manifest layer, got nil")
	}
}

func TestOCIPackageStore_MalformedInnerManifest(t *testing.T) {
	reg := newStubRegistry()
	badManifest := []byte("{not valid json")
	blobBytes := []byte("irrelevant")
	reg.publish("hermes/provisioning/office-docx", "1.4.0", badManifest, blobBytes)
	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	_, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() expected error for malformed inner manifest.json, got nil")
	}
}

func TestOCIPackageStore_List(t *testing.T) {
	reg := newStubRegistry()
	a := ociFixtureManifest()
	aJSON, _ := json.Marshal(a)
	reg.publish("hermes/provisioning/office-docx", "1.4.0", aJSON, []byte("blob-a"))

	b := ociFixtureManifest()
	b.Name = "office-xlsx"
	b.Version = "1.0.0"
	bJSON, _ := json.Marshal(b)
	reg.publish("hermes/provisioning/office-xlsx", "1.0.0", bJSON, []byte("blob-b"))

	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d entries, want 2: %+v", len(got), got)
	}
}

func TestOCIPackageStore_List_Empty(t *testing.T) {
	reg := newStubRegistry()
	srv := reg.server(t)
	store := &OCIPackageStore{BaseURL: srv.URL, Repository: "hermes/provisioning", HTTPClient: srv.Client()}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List() = %+v, want empty", got)
	}
}

func TestNewOCIPackageStoreFromEnv(t *testing.T) {
	t.Run("unset returns nil", func(t *testing.T) {
		t.Setenv("GOOSAR_PROVISIONING_OCI_URL", "")
		if store := NewOCIPackageStoreFromEnv(); store != nil {
			t.Fatalf("NewOCIPackageStoreFromEnv() = %+v, want nil", store)
		}
	})

	t.Run("set builds a store", func(t *testing.T) {
		t.Setenv("GOOSAR_PROVISIONING_OCI_URL", "https://registry.internal:5000/")
		t.Setenv("GOOSAR_PROVISIONING_OCI_REPOSITORY", "/hermes/provisioning/")
		store := NewOCIPackageStoreFromEnv()
		if store == nil {
			t.Fatal("NewOCIPackageStoreFromEnv() = nil, want a store")
		}
		if store.BaseURL != "https://registry.internal:5000" {
			t.Errorf("BaseURL = %q", store.BaseURL)
		}
		if store.Repository != "hermes/provisioning" {
			t.Errorf("Repository = %q", store.Repository)
		}
	})
}

func TestOCIPackageStore_RepoPath(t *testing.T) {
	cases := []struct {
		repository string
		name       string
		want       string
	}{
		{"", "office-docx", "office-docx"},
		{"hermes/provisioning", "office-docx", "hermes/provisioning/office-docx"},
	}
	for _, tc := range cases {
		store := &OCIPackageStore{Repository: tc.repository}
		if got := store.repoPath(tc.name); got != tc.want {
			t.Errorf("repoPath(%q) with Repository=%q = %q, want %q", tc.name, tc.repository, got, tc.want)
		}
	}
}

func TestOCIPackageStore_Unreachable(t *testing.T) {
	store := &OCIPackageStore{BaseURL: "http://127.0.0.1:1", Repository: "x", HTTPClient: &http.Client{}}
	_, err := store.Manifest(context.Background(), "office-docx", "1.4.0")
	if err == nil {
		t.Fatal("Manifest() expected error for unreachable registry, got nil")
	}

	if msg := fmt.Sprintf("%v", err); msg == "" {
		t.Fatal("error message is empty")
	}
}
