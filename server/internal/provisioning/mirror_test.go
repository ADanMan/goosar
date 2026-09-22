package provisioning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeSourceStore struct {
	manifests map[string]PackageManifest
	blobs     map[string][]byte
	listErr   error
	blobErr   error
}

func newFakeSourceStore() *fakeSourceStore {
	return &fakeSourceStore{
		manifests: map[string]PackageManifest{},
		blobs:     map[string][]byte{},
	}
}

func (s *fakeSourceStore) key(name, version string) string { return name + "@" + version }

func (s *fakeSourceStore) add(m PackageManifest, blob []byte) {
	s.manifests[s.key(m.Name, m.Version)] = m
	s.blobs[s.key(m.Name, m.Version)] = blob
}

func (s *fakeSourceStore) Manifest(_ context.Context, name, version string) (PackageManifest, error) {
	m, ok := s.manifests[s.key(name, version)]
	if !ok {
		return PackageManifest{}, ErrPackageNotFound
	}
	return m, nil
}

func (s *fakeSourceStore) Blob(_ context.Context, name, version string) (io.ReadCloser, int64, error) {
	if s.blobErr != nil {
		return nil, 0, s.blobErr
	}
	b, ok := s.blobs[s.key(name, version)]
	if !ok {
		return nil, 0, ErrPackageNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func (s *fakeSourceStore) List(_ context.Context) ([]PackageManifest, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]PackageManifest, 0, len(s.manifests))
	for _, m := range s.manifests {
		out = append(out, m)
	}
	return out, nil
}

func manifestFor(name, version string, blob []byte) PackageManifest {
	sum := sha256.Sum256(blob)
	return PackageManifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          name,
		Version:       version,
		Type:          PackageTypeSkill,
		Platform:      "*",
		SHA256:        hex.EncodeToString(sum[:]),
		Size:          int64(len(blob)),
	}
}

func TestMirrorToLocal_CopiesAndServes(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	blobA := []byte("package A payload")
	blobB := []byte("package B payload")
	src.add(manifestFor("office-docx", "1.4.0", blobA), blobA)
	src.add(manifestFor("jira-mcp", "0.2.1", blobB), blobB)

	n, err := MirrorToLocal(context.Background(), src, dst, nil)
	if err != nil {
		t.Fatalf("MirrorToLocal() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("MirrorToLocal() copied %d packages, want 2", n)
	}

	listed, err := dst.List(context.Background())
	if err != nil {
		t.Fatalf("local List() after mirror: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("local catalog has %d entries, want 2", len(listed))
	}

	reader, size, err := dst.Blob(context.Background(), "office-docx", "1.4.0")
	if err != nil {
		t.Fatalf("local Blob() after mirror: %v", err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, blobA) {
		t.Fatalf("mirrored blob = %q, want %q", got, blobA)
	}
	if size != int64(len(blobA)) {
		t.Fatalf("mirrored blob size = %d, want %d", size, len(blobA))
	}
}

func TestMirrorToLocal_RejectsChecksumMismatch(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	m := manifestFor("office-docx", "1.4.0", []byte("the real payload"))

	src.add(m, []byte("the fake payload"))

	_, err := MirrorToLocal(context.Background(), src, dst, nil)
	if err == nil {
		t.Fatal("MirrorToLocal() accepted a blob whose sha256 does not match its manifest")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("MirrorToLocal() error = %v, want it to name the sha256 mismatch", err)
	}
	if _, listErr := dst.List(context.Background()); !errors.Is(listErr, ErrCatalogNotFound) {
		t.Fatalf("local List() after a rejected mirror = %v, want ErrCatalogNotFound (no catalog written)", listErr)
	}
}

func TestMirrorToLocal_RejectsSizeMismatch(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	blob := []byte("payload")
	m := manifestFor("office-docx", "1.4.0", blob)
	m.Size = m.Size + 10
	src.add(m, blob)

	_, err := MirrorToLocal(context.Background(), src, dst, nil)
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("MirrorToLocal() error = %v, want a size-mismatch refusal", err)
	}
}

func TestMirrorToLocal_ExplicitRefs(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	src.listErr = errors.New("registry does not implement _catalog")
	blob := []byte("payload")
	src.add(manifestFor("office-docx", "1.4.0", blob), blob)

	n, err := MirrorToLocal(context.Background(), src, dst, []PackageRef{{Name: "office-docx", Version: "1.4.0"}})
	if err != nil {
		t.Fatalf("MirrorToLocal() with explicit refs error = %v", err)
	}
	if n != 1 {
		t.Fatalf("MirrorToLocal() copied %d, want 1", n)
	}
}

func TestParsePackageRefs(t *testing.T) {
	refs, err := ParsePackageRefs(" office-docx@1.4.0 , jira-mcp@0.2.1 ")
	if err != nil {
		t.Fatalf("ParsePackageRefs() error = %v", err)
	}
	want := []PackageRef{{Name: "office-docx", Version: "1.4.0"}, {Name: "jira-mcp", Version: "0.2.1"}}
	if len(refs) != len(want) {
		t.Fatalf("ParsePackageRefs() = %v, want %v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("ParsePackageRefs()[%d] = %v, want %v", i, refs[i], want[i])
		}
	}
	if _, err := ParsePackageRefs("no-version-here"); err == nil {
		t.Fatal("ParsePackageRefs() accepted a ref with no @version")
	}
	if _, err := ParsePackageRefs("../evil@1.0.0"); err == nil {
		t.Fatal("ParsePackageRefs() accepted a path-traversal package name")
	}
}

func captureLogs(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestEnsureLocalKit_SyncsOnFirstBoot(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	blob := []byte("payload")
	src.add(manifestFor("office-docx", "1.4.0", blob), blob)

	var logs bytes.Buffer
	served := EnsureLocalKit(context.Background(), src, dst, nil, captureLogs(&logs))
	if served != PackageStore(dst) {
		t.Fatal("EnsureLocalKit() served from the registry after a successful sync, want the local mirror")
	}
	listed, err := dst.List(context.Background())
	if err != nil || len(listed) != 1 {
		t.Fatalf("local catalog after first-boot sync = %v (err %v), want 1 package", listed, err)
	}
}

func TestEnsureLocalKit_KeepsExistingKit(t *testing.T) {
	dst, backend := newTestLocalStore(t)
	fixture := fixtureManifest()
	publishFixture(t, backend, "provisioning", fixture)
	publishCatalog(t, backend, "provisioning", []catalogEntry{{Name: fixture.Name, Version: fixture.Version}})

	src := newFakeSourceStore()
	other := []byte("a different payload entirely")
	src.add(manifestFor("jira-mcp", "0.2.1", other), other)

	served := EnsureLocalKit(context.Background(), src, dst, nil, captureLogs(&bytes.Buffer{}))
	if served != PackageStore(dst) {
		t.Fatal("EnsureLocalKit() did not serve the already-present local kit")
	}
	listed, err := dst.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != fixture.Name {
		t.Fatalf("existing kit was modified: %v (err %v)", listed, err)
	}
}

func TestEnsureLocalKit_FallsBackToRegistry(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	src := newFakeSourceStore()
	blob := []byte("payload")
	src.add(manifestFor("office-docx", "1.4.0", blob), blob)
	src.blobErr = errors.New("registry blob read failed")

	served := EnsureLocalKit(context.Background(), src, dst, nil, captureLogs(&bytes.Buffer{}))
	if served != PackageStore(src) {
		t.Fatal("EnsureLocalKit() did not fall back to the registry after a failed mirror")
	}
}

func TestEnsureLocalKit_LogsHintWhenNothingConfigured(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	var logs bytes.Buffer
	served := EnsureLocalKit(context.Background(), nil, dst, nil, captureLogs(&logs))
	if served != PackageStore(dst) {
		t.Fatal("EnsureLocalKit() with no source should still serve the (empty) local store")
	}
	for _, want := range []string{"make selfhost-packages", "load-provisioning-packages.sh", "GOOSAR_PROVISIONING_OCI_URL"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing-kit log does not mention %q; got:\n%s", want, logs.String())
		}
	}
}

type blackholeStore struct{}

func (blackholeStore) Manifest(ctx context.Context, _, _ string) (PackageManifest, error) {
	<-ctx.Done()
	return PackageManifest{}, ctx.Err()
}

func (blackholeStore) Blob(ctx context.Context, _, _ string) (io.ReadCloser, int64, error) {
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

func (blackholeStore) List(ctx context.Context) ([]PackageManifest, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEnsureLocalKit_UnreachableRegistryDoesNotBlockStartup(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	const budget = 150 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	done := make(chan PackageStore, 1)
	start := time.Now()
	go func() { done <- EnsureLocalKit(ctx, blackholeStore{}, dst, nil, captureLogs(&bytes.Buffer{})) }()

	var served PackageStore
	select {
	case served = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("EnsureLocalKit() did not return after its context expired — an unreachable registry hangs startup")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("EnsureLocalKit() took %s against an unreachable registry, budget was %s", elapsed, budget)
	}
	if _, ok := served.(blackholeStore); !ok {
		t.Fatalf("EnsureLocalKit() served %T, want the fallback to the configured registry", served)
	}
	if _, err := dst.List(context.Background()); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("local catalog after a failed sync = %v, want no catalog at all so the next boot retries", err)
	}
}

func TestEnsureLocalKit_AlreadyExpiredContext(t *testing.T) {
	dst, _ := newTestLocalStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if served := EnsureLocalKit(ctx, blackholeStore{}, dst, nil, captureLogs(&bytes.Buffer{})); served == nil {
		t.Fatal("EnsureLocalKit() returned a nil store on an expired context; provisioning handlers would panic")
	}
}
