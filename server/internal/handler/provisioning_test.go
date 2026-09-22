package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/provisioning"
)

type fakeProvisioningStore struct {
	manifests map[string]provisioning.PackageManifest
	blobs     map[string][]byte
	malformed map[string]bool

	catalogNotFound bool

	listErr error
}

func newFakeProvisioningStore() *fakeProvisioningStore {
	return &fakeProvisioningStore{
		manifests: map[string]provisioning.PackageManifest{},
		blobs:     map[string][]byte{},
		malformed: map[string]bool{},
	}
}

func fakeStoreKey(name, version string) string { return name + "@" + version }

func (s *fakeProvisioningStore) add(m provisioning.PackageManifest, blob []byte) {
	s.manifests[fakeStoreKey(m.Name, m.Version)] = m
	s.blobs[fakeStoreKey(m.Name, m.Version)] = blob
}

func (s *fakeProvisioningStore) addMalformed(name, version string) {
	s.malformed[fakeStoreKey(name, version)] = true
}

func (s *fakeProvisioningStore) Manifest(_ context.Context, name, version string) (provisioning.PackageManifest, error) {
	key := fakeStoreKey(name, version)
	if s.malformed[key] {
		return provisioning.PackageManifest{}, fmt.Errorf("%w: fake malformed manifest for %s@%s", provisioning.ErrMalformedManifest, name, version)
	}
	m, ok := s.manifests[key]
	if !ok {
		return provisioning.PackageManifest{}, fmt.Errorf("%w: %s@%s", provisioning.ErrPackageNotFound, name, version)
	}
	return m, nil
}

func (s *fakeProvisioningStore) Blob(_ context.Context, name, version string) (io.ReadCloser, int64, error) {
	key := fakeStoreKey(name, version)
	data, ok := s.blobs[key]
	if !ok {
		return nil, 0, fmt.Errorf("%w: blob %s@%s", provisioning.ErrPackageNotFound, name, version)
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (s *fakeProvisioningStore) List(_ context.Context) ([]provisioning.PackageManifest, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.catalogNotFound {
		return nil, fmt.Errorf("%w: catalog: fake missing catalog.json", provisioning.ErrCatalogNotFound)
	}
	out := make([]provisioning.PackageManifest, 0, len(s.manifests))
	for _, m := range s.manifests {
		out = append(out, m)
	}
	return out, nil
}

func withProvisioningStore(t *testing.T, store provisioning.PackageStore) {
	t.Helper()
	prev := testHandler.ProvisioningStore
	testHandler.ProvisioningStore = store
	t.Cleanup(func() { testHandler.ProvisioningStore = prev })
}

func fixturePackage(pkgType, name, version, platform string, requires ...string) (provisioning.PackageManifest, []byte) {
	blob := []byte("fake tar.zst bytes for " + name + "@" + version)
	sum := sha256.Sum256(blob)
	m := provisioning.PackageManifest{
		SchemaVersion: provisioning.CurrentSchemaVersion,
		Name:          name,
		Version:       version,
		Type:          pkgType,
		Platform:      platform,
		SHA256:        hex.EncodeToString(sum[:]),
		Size:          int64(len(blob)),
		Requires:      requires,
	}
	return m, blob
}

func insertProvisioningPinFixture(t *testing.T, workspaceID, name, pkgType, version string, enabled bool) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO provisioning_pin (workspace_id, package_name, package_type, version, enabled)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, package_name, package_type)
		DO UPDATE SET version = EXCLUDED.version, enabled = EXCLUDED.enabled
	`, workspaceID, name, pkgType, version, enabled); err != nil {
		t.Fatalf("insert provisioning_pin fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM provisioning_pin WHERE workspace_id = $1 AND package_name = $2 AND package_type = $3`,
			workspaceID, name, pkgType)
	})
}

func secondWorkspaceFixture(t *testing.T) (workspaceID, ownerID string) {
	t.Helper()
	ctx := context.Background()
	suffix := randomID()[:8]

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Provisioning Isolation Owner', $1)
		RETURNING id
	`, "provisioning-isolation-owner-"+suffix+"@goosar.test").Scan(&ownerID); err != nil {
		t.Fatalf("create isolation workspace owner: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID) })

	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Provisioning Isolation WS', $1, 'tmp', 'PIW')
		RETURNING id
	`, "provisioning-isolation-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create isolation workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM provisioning_pin WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, ownerID); err != nil {
		t.Fatalf("add isolation workspace owner member: %v", err)
	}
	return workspaceID, ownerID
}

func nonAdminMemberFixture(t *testing.T) (userID string) {
	t.Helper()
	ctx := context.Background()
	suffix := randomID()[:8]
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Provisioning Plain Member', $1)
		RETURNING id
	`, "provisioning-plain-member-"+suffix+"@goosar.test").Scan(&userID); err != nil {
		t.Fatalf("create plain member user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add plain member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	})
	return userID
}

func withProvisioningBlobParams(req *http.Request, name, version string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func decodeManifestResponse(t *testing.T, body *bytes.Buffer) ProvisioningManifestResponse {
	t.Helper()
	var resp ProvisioningManifestResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode manifest response: %v", err)
	}
	return resp
}

func manifestPackageNames(resp ProvisioningManifestResponse) []string {
	names := make([]string, len(resp.Packages))
	for i, p := range resp.Packages {
		names[i] = p.Name
	}
	return names
}

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

func TestGetProvisioningManifest_StoreUnconfigured(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withProvisioningStore(t, nil)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningManifest_MissingPlatform(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withProvisioningStore(t, newFakeProvisioningStore())

	req := newRequest(http.MethodGet, "/api/provisioning/manifest", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningManifest_InvalidPlatform(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withProvisioningStore(t, newFakeProvisioningStore())

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=amiga-68k", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningManifest_RequiresResolutionAndPlatformFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	runtime, runtimeBlob := fixturePackage(provisioning.PackageTypeRuntime, "playwright-browsers", "2.0.0", "*")
	mcp, mcpBlob := fixturePackage(provisioning.PackageTypeMCPServer, "office", "1.0.0", "linux-x64")
	skill, skillBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*",
		"runtime:playwright-browsers@2.0.0", "mcp-server:office@1.0.0")
	store.add(runtime, runtimeBlob)
	store.add(mcp, mcpBlob)
	store.add(skill, skillBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, skill.Name, skill.Type, skill.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	names := manifestPackageNames(resp)
	for _, want := range []string{"playwright-browsers", "office", "office-docx"} {
		if !containsName(names, want) {
			t.Fatalf("linux-x64 manifest missing %q: got %v", want, names)
		}
	}
	if resp.SchemaVersion != provisioning.CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", resp.SchemaVersion, provisioning.CurrentSchemaVersion)
	}

	req2 := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w2 := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w2.Code, w2.Body.String())
	}
	resp2 := decodeManifestResponse(t, w2.Body)
	names2 := manifestPackageNames(resp2)
	if containsName(names2, "office") {
		t.Fatalf("darwin-arm64 manifest leaked linux-only package: %v", names2)
	}
	if !containsName(names2, "playwright-browsers") || !containsName(names2, "office-docx") {
		t.Fatalf("darwin-arm64 manifest missing platform-agnostic packages: %v", names2)
	}
}

func TestGetProvisioningManifest_PlatformUncoveredIsNotEmptyCatalog(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	mcp, mcpBlob := fixturePackage(provisioning.PackageTypeMCPServer, "office", "1.0.0", "darwin-arm64")
	mcp2, mcp2Blob := fixturePackage(provisioning.PackageTypeMCPServer, "fetch", "1.0.0", "linux-x64")
	store.add(mcp, mcpBlob)
	store.add(mcp2, mcp2Blob)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=win-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if len(resp.Packages) != 0 {
		t.Fatalf("win-x64 packages = %v, want none", manifestPackageNames(resp))
	}
	if resp.TotalBeforePlatformFilter != 2 {
		t.Fatalf("TotalBeforePlatformFilter = %d, want 2", resp.TotalBeforePlatformFilter)
	}
	if got := strings.Join(resp.PlatformsAvailable, ","); got != "darwin-arm64,linux-x64" {
		t.Fatalf("PlatformsAvailable = %q, want darwin-arm64,linux-x64", got)
	}

	req2 := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-arm64", nil)
	w2 := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("linux-arm64 status = %d, want 200: %s", w2.Code, w2.Body.String())
	}
}

func TestGetProvisioningManifest_MissingDependency(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	skill, blob := fixturePackage(provisioning.PackageTypeSkill, "broken-deps", "1.0.0", "*", "runtime:ghost@9.9.9")
	store.add(skill, blob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, skill.Name, skill.Type, skill.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with unavailablePackages: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if containsName(manifestPackageNames(resp), "broken-deps") {
		t.Fatalf("manifest served a package with an unresolvable dependency: %v", resp.Packages)
	}
	if len(resp.UnavailablePackages) != 1 {
		t.Fatalf("UnavailablePackages = %+v, want 1 entry", resp.UnavailablePackages)
	}
	if resp.UnavailablePackages[0].Key != "skill:broken-deps@1.0.0" {
		t.Fatalf("UnavailablePackages[0].Key = %q, want broken-deps root key", resp.UnavailablePackages[0].Key)
	}
	wantReason := "dependency unavailable: broken-deps requires ghost@9.9.9"
	if resp.UnavailablePackages[0].Reason != wantReason {
		t.Fatalf("UnavailablePackages[0].Reason = %q, want %q", resp.UnavailablePackages[0].Reason, wantReason)
	}
}

func TestGetProvisioningManifest_HRWorkspaceEwsMcpUnavailable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	mailTriage, mailBlob := fixturePackage(provisioning.PackageTypeSkill, "mail-triage", "1.0.0", "*", "mcp-server:ews-mcp@0.1.0")

	officeMCP, officeMCPBlob := fixturePackage(provisioning.PackageTypeMCPServer, "office", "1.0.0", "linux-x64")
	officeDocx, officeDocxBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.0.0", "*", "mcp-server:office@1.0.0")
	store.add(mailTriage, mailBlob)
	store.add(officeMCP, officeMCPBlob)
	store.add(officeDocx, officeDocxBlob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, mailTriage.Name, mailTriage.Type, mailTriage.Version, true)
	insertProvisioningPinFixture(t, testWorkspaceID, officeDocx.Name, officeDocx.Type, officeDocx.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	names := manifestPackageNames(resp)
	if !containsName(names, "office-docx") || !containsName(names, "office") {
		t.Fatalf("manifest = %v, want office-docx and office delivered", names)
	}
	if containsName(names, "mail-triage") {
		t.Fatalf("manifest served mail-triage despite its MCP being unavailable: %v", names)
	}
	if len(resp.UnavailablePackages) != 1 || resp.UnavailablePackages[0].Key != "skill:mail-triage@1.0.0" {
		t.Fatalf("UnavailablePackages = %+v, want mail-triage excluded", resp.UnavailablePackages)
	}
}

func TestGetProvisioningManifest_MissingRootPinUnavailable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	other, otherBlob := fixturePackage(provisioning.PackageTypeSkill, "still-here", "1.0.0", "*")
	store.add(other, otherBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, "ghost-pkg", provisioning.PackageTypeSkill, "1.0.0", true)
	insertProvisioningPinFixture(t, testWorkspaceID, other.Name, other.Type, other.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if !containsName(manifestPackageNames(resp), "still-here") {
		t.Fatalf("manifest = %v, want still-here delivered", manifestPackageNames(resp))
	}
	if len(resp.UnavailablePackages) != 1 {
		t.Fatalf("UnavailablePackages = %+v, want 1 entry", resp.UnavailablePackages)
	}
	wantReason := "package not found: ghost-pkg@1.0.0"
	if resp.UnavailablePackages[0].Reason != wantReason {
		t.Fatalf("UnavailablePackages[0].Reason = %q, want %q", resp.UnavailablePackages[0].Reason, wantReason)
	}
}

func TestGetProvisioningManifest_RequiresCycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	a, aBlob := fixturePackage(provisioning.PackageTypeSkill, "cycle-a", "1.0.0", "*", "skill:cycle-b@1.0.0")
	b, bBlob := fixturePackage(provisioning.PackageTypeSkill, "cycle-b", "1.0.0", "*", "skill:cycle-a@1.0.0")
	store.add(a, aBlob)
	store.add(b, bBlob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, a.Name, a.Type, a.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("status = 200, want a failure status for a requires cycle: %s", w.Body.String())
	}
}

func TestGetProvisioningManifest_MalformedManifest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	store.addMalformed("broken-manifest", "1.0.0")
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, "broken-manifest", provisioning.PackageTypeSkill, "1.0.0", true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("GetProvisioningManifest panicked on malformed manifest: %v", r)
			}
		}()
		testHandler.GetProvisioningManifest(w, req)
	}()

	if w.Code == http.StatusOK {
		t.Fatalf("status = 200, want a failure status for a malformed manifest: %s", w.Body.String())
	}
}

func TestGetProvisioningManifest_DisabledPinExcluded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	skill, blob := fixturePackage(provisioning.PackageTypeSkill, "disabled-pkg", "1.0.0", "*")
	store.add(skill, blob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, skill.Name, skill.Type, skill.Version, false)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if containsName(manifestPackageNames(resp), "disabled-pkg") {
		t.Fatalf("manifest included a disabled pin: %v", resp.Packages)
	}
}

func TestGetProvisioningBlob_Success(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	skill, blob := fixturePackage(provisioning.PackageTypeSkill, "blob-pkg", "1.0.0", "*")
	store.add(skill, blob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, skill.Name, skill.Type, skill.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/blob/"+skill.Name+"/"+skill.Version+"?platform=linux-x64", nil)
	req = withProvisioningBlobParams(req, skill.Name, skill.Version)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zstd" {
		t.Errorf("Content-Type = %q, want application/zstd", got)
	}
	if got := w.Header().Get("X-Package-Sha256"); got != skill.SHA256 {
		t.Errorf("X-Package-Sha256 = %q, want %q", got, skill.SHA256)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if w.Body.String() != string(blob) {
		t.Errorf("body = %q, want %q", w.Body.String(), blob)
	}
}

func TestGetProvisioningBlob_NotInStore(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/blob/never-published/1.0.0?platform=linux-x64", nil)
	req = withProvisioningBlobParams(req, "never-published", "1.0.0")
	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningBlob_NoPinsServesFromWholeCatalog(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	skill, blob := fixturePackage(provisioning.PackageTypeSkill, "catalog-pkg", "1.0.0", "*")
	store.add(skill, blob)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/blob/"+skill.Name+"/"+skill.Version+"?platform=linux-x64", nil)
	req = withProvisioningBlobParams(req, skill.Name, skill.Version)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != string(blob) {
		t.Errorf("body = %q, want %q", w.Body.String(), blob)
	}
}

func TestGetProvisioningBlob_WorkspaceIsolation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	secretPkg, secretBlob := fixturePackage(provisioning.PackageTypeSkill, "secret-ws-b-pkg", "1.0.0", "*")
	ownPkg, ownBlob := fixturePackage(provisioning.PackageTypeSkill, "workspace-a-own-pkg", "1.0.0", "*")
	store.add(secretPkg, secretBlob)
	store.add(ownPkg, ownBlob)
	withProvisioningStore(t, store)

	wsB, _ := secondWorkspaceFixture(t)
	insertProvisioningPinFixture(t, wsB, secretPkg.Name, secretPkg.Type, secretPkg.Version, true)

	insertProvisioningPinFixture(t, testWorkspaceID, ownPkg.Name, ownPkg.Type, ownPkg.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/blob/"+secretPkg.Name+"/"+secretPkg.Version+"?platform=linux-x64", nil)
	req = withProvisioningBlobParams(req, secretPkg.Name, secretPkg.Version)

	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace blob pull: status = %d, want 404 (isolation must fail closed): %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), secretBlob) {
		t.Fatal("cross-workspace blob pull leaked the blob content")
	}
}

func TestGetProvisioningBlob_PlatformMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	pkg, blob := fixturePackage(provisioning.PackageTypeRuntime, "darwin-only-pkg", "1.0.0", "darwin-arm64")
	store.add(pkg, blob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, pkg.Name, pkg.Type, pkg.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/blob/"+pkg.Name+"/"+pkg.Version+"?platform=linux-x64", nil)
	req = withProvisioningBlobParams(req, pkg.Name, pkg.Version)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("platform-mismatched blob pull: status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningPins_RequiresAdminRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	memberID := nonAdminMemberFixture(t)

	req := newRequestAs(memberID, http.MethodGet, "/api/provisioning/pins", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningPins(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member GetProvisioningPins: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningPins_OwnerSucceeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	insertProvisioningPinFixture(t, testWorkspaceID, "pins-list-pkg", provisioning.PackageTypeSkill, "1.0.0", true)

	req := newRequest(http.MethodGet, "/api/provisioning/pins", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningPins(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("owner GetProvisioningPins: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ProvisioningPinsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode pins response: %v", err)
	}
	found := false
	for _, p := range resp.Pins {
		if p.PackageName == "pins-list-pkg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pins response missing fixture pin: %+v", resp.Pins)
	}
}

func TestPutProvisioningPins_RequiresAdminRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	memberID := nonAdminMemberFixture(t)

	req := newRequestAs(memberID, http.MethodPut, "/api/provisioning/pins", map[string]any{
		"pins": []map[string]any{
			{"package_name": "should-not-write", "package_type": "skill", "version": "1.0.0", "enabled": true},
		},
	})
	w := httptest.NewRecorder()
	testHandler.PutProvisioningPins(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member PutProvisioningPins: status = %d, want 403: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM provisioning_pin WHERE workspace_id = $1 AND package_name = 'should-not-write'`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected PUT still wrote a pin row (count=%d)", count)
	}
}

func TestPutProvisioningPins_ReplacesWholeLockfile(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM provisioning_pin WHERE workspace_id = $1 AND package_name IN ('replace-a', 'replace-b')`, testWorkspaceID)
	})

	putReq := newRequest(http.MethodPut, "/api/provisioning/pins", map[string]any{
		"pins": []map[string]any{
			{"package_name": "replace-a", "package_type": "skill", "version": "1.0.0", "enabled": true},
		},
	})
	w := httptest.NewRecorder()
	testHandler.PutProvisioningPins(w, putReq)
	if w.Code != http.StatusOK {
		t.Fatalf("first PUT: status = %d, want 200: %s", w.Code, w.Body.String())
	}

	putReq2 := newRequest(http.MethodPut, "/api/provisioning/pins", map[string]any{
		"pins": []map[string]any{
			{"package_name": "replace-b", "package_type": "skill", "version": "1.0.0", "enabled": true},
		},
	})
	w2 := httptest.NewRecorder()
	testHandler.PutProvisioningPins(w2, putReq2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT: status = %d, want 200: %s", w2.Code, w2.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM provisioning_pin WHERE workspace_id = $1 AND package_name = 'replace-a'`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("whole-lockfile PUT left the old pin in place (count=%d)", count)
	}
}

func TestPutProvisioningPins_RejectsInvalidPackageType(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := newRequest(http.MethodPut, "/api/provisioning/pins", map[string]any{
		"pins": []map[string]any{
			{"package_name": "x", "package_type": "malware", "version": "1.0.0", "enabled": true},
		},
	})
	w := httptest.NewRecorder()
	testHandler.PutProvisioningPins(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestPutProvisioningPins_MalformedBody(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := httptest.NewRequest(http.MethodPut, "/api/provisioning/pins", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.PutProvisioningPins(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestProvisioningPins_RejectTaskTokenActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	r := chi.NewRouter()
	r.With(RequireHumanActor).Get("/api/provisioning/pins", testHandler.GetProvisioningPins)
	r.With(RequireHumanActor).Put("/api/provisioning/pins", testHandler.PutProvisioningPins)

	t.Run("GET rejects task_token actor", func(t *testing.T) {
		req := newRequest(http.MethodGet, "/api/provisioning/pins", nil)
		req.Header.Set("X-Actor-Source", "task_token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	t.Run("PUT rejects task_token actor", func(t *testing.T) {
		req := newRequest(http.MethodPut, "/api/provisioning/pins", map[string]any{
			"pins": []map[string]any{
				{"package_name": "task-token-should-not-write", "package_type": "skill", "version": "1.0.0", "enabled": true},
			},
		})
		req.Header.Set("X-Actor-Source", "task_token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}

		var count int
		if err := testPool.QueryRow(context.Background(),
			`SELECT count(*) FROM provisioning_pin WHERE workspace_id = $1 AND package_name = 'task-token-should-not-write'`,
			testWorkspaceID).Scan(&count); err != nil {
			t.Fatalf("count query: %v", err)
		}
		if count != 0 {
			t.Fatalf("rejected task-token PUT still wrote a pin row (count=%d)", count)
		}
	})

	t.Run("GET allows human actor (owner)", func(t *testing.T) {
		req := newRequest(http.MethodGet, "/api/provisioning/pins", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
	})
}

func TestGetProvisioningManifest_NoPinsServesWholeCatalog(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	runtime, runtimeBlob := fixturePackage(provisioning.PackageTypeRuntime, "playwright-browsers", "2.0.0", "*")
	mcp, mcpBlob := fixturePackage(provisioning.PackageTypeMCPServer, "outlook", "1.2.0", "darwin-arm64")
	skill, skillBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*")
	store.add(runtime, runtimeBlob)
	store.add(mcp, mcpBlob)
	store.add(skill, skillBlob)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	names := manifestPackageNames(resp)
	for _, want := range []string{"office-docx", "outlook", "playwright-browsers"} {
		if !containsName(names, want) {
			t.Fatalf("unpinned workspace did not receive %q from the catalog: got %v", want, names)
		}
	}
}

func TestGetProvisioningManifest_PinsStillNarrow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	wanted, wantedBlob := fixturePackage(provisioning.PackageTypeSkill, "policy-lookup", "1.0.0", "*")
	other, otherBlob := fixturePackage(provisioning.PackageTypeSkill, "research", "1.0.0", "*")
	store.add(wanted, wantedBlob)
	store.add(other, otherBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, wanted.Name, wanted.Type, wanted.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	names := manifestPackageNames(decodeManifestResponse(t, w.Body))
	if !containsName(names, "policy-lookup") {
		t.Fatalf("pinned package missing: %v", names)
	}
	if containsName(names, "research") {
		t.Fatalf("unpinned package leaked into a curated workspace: %v", names)
	}
}

func TestGetProvisioningManifest_DisabledOnlyPinDoesNotFallBack(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	disabled, disabledBlob := fixturePackage(provisioning.PackageTypeSkill, "disabled-only-pkg", "1.0.0", "*")
	other, otherBlob := fixturePackage(provisioning.PackageTypeSkill, "catalog-only-pkg", "1.0.0", "*")
	store.add(disabled, disabledBlob)
	store.add(other, otherBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, disabled.Name, disabled.Type, disabled.Version, false)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if len(resp.Packages) != 0 {
		t.Fatalf("workspace with only a disabled pin should get an empty manifest, not the whole catalog: %v", resp.Packages)
	}
}

func TestGetProvisioningManifest_NoPinsCatalogNotFoundServesEmptyList(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	store.catalogNotFound = true
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a deployment with no catalog published yet: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	if len(resp.Packages) != 0 {
		t.Fatalf("want an empty package list, got %v", resp.Packages)
	}
}

func TestGetProvisioningManifest_NoPinsCatalogListErrorFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	store.listErr = fmt.Errorf("%w: catalog: fake malformed catalog.json", provisioning.ErrMalformedManifest)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("status = 200, want a failure status for a broken catalog: %s", w.Body.String())
	}
}

func TestGetProvisioningManifest_NoPinsCatalogRequiresResolvedNoDuplicates(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	runtime, runtimeBlob := fixturePackage(provisioning.PackageTypeRuntime, "shared-runtime", "1.0.0", "*")
	skill, skillBlob := fixturePackage(provisioning.PackageTypeSkill, "needs-shared-runtime", "1.0.0", "*",
		"runtime:shared-runtime@1.0.0")
	store.add(runtime, runtimeBlob)
	store.add(skill, skillBlob)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=linux-x64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	resp := decodeManifestResponse(t, w.Body)
	count := 0
	for _, p := range resp.Packages {
		if p.Name == "shared-runtime" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared-runtime appeared %d times in the resolved manifest, want exactly 1: %v", count, resp.Packages)
	}
}

func TestGetProvisioningManifest_NoPinsPlatformFilterApplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	linuxOnly, linuxBlob := fixturePackage(provisioning.PackageTypeRuntime, "linux-only-catalog-pkg", "1.0.0", "linux-x64")
	agnostic, agnosticBlob := fixturePackage(provisioning.PackageTypeSkill, "cross-platform-catalog-pkg", "1.0.0", "*")
	store.add(linuxOnly, linuxBlob)
	store.add(agnostic, agnosticBlob)
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	names := manifestPackageNames(decodeManifestResponse(t, w.Body))
	if containsName(names, "linux-only-catalog-pkg") {
		t.Fatalf("darwin-arm64 manifest leaked a linux-only catalog package via the no-pins fallback: %v", names)
	}
	if !containsName(names, "cross-platform-catalog-pkg") {
		t.Fatalf("darwin-arm64 manifest missing a platform-agnostic catalog package: %v", names)
	}
}

func TestGetProvisioningCatalog_RequiresAdminRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	withProvisioningStore(t, store)
	memberID := nonAdminMemberFixture(t)

	req := newRequestAs(memberID, http.MethodGet, "/api/provisioning/catalog", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningCatalog(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member GetProvisioningCatalog: status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningCatalog_ListsWholeCatalogDespitePins(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	pinned, pinnedBlob := fixturePackage(provisioning.PackageTypeSkill, "catalog-pinned-pkg", "1.0.0", "any")
	other, otherBlob := fixturePackage(provisioning.PackageTypeSkill, "catalog-other-pkg", "2.0.0", "any")
	store.add(pinned, pinnedBlob)
	store.add(other, otherBlob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, pinned.Name, pinned.Type, pinned.Version, true)

	req := newRequest(http.MethodGet, "/api/provisioning/catalog", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningCatalog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("owner GetProvisioningCatalog: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ProvisioningCatalogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	found := map[string]bool{}
	for _, p := range resp.Packages {
		found[p.Name] = true
	}
	if !found[pinned.Name] || !found[other.Name] {
		t.Fatalf("catalog response = %+v, want both %q and %q despite pins", resp.Packages, pinned.Name, other.Name)
	}
}

func TestGetProvisioningCatalog_EmptyWhenNothingPublished(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	store.catalogNotFound = true
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/catalog", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningCatalog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("empty deployment GetProvisioningCatalog: status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ProvisioningCatalogResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if len(resp.Packages) != 0 {
		t.Fatalf("empty deployment catalog: packages = %+v, want none", resp.Packages)
	}

	if resp.Packages == nil {
		t.Fatal("empty deployment catalog: packages must be [], not null")
	}
}

func TestGetProvisioningCatalog_UnconfiguredStoreAnswers503(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withProvisioningStore(t, nil)

	req := newRequest(http.MethodGet, "/api/provisioning/catalog", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningCatalog(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured store GetProvisioningCatalog: status = %d, want 503: %s", w.Code, w.Body.String())
	}
}

func TestGetProvisioningCatalog_ListFailureFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := newFakeProvisioningStore()
	store.listErr = fmt.Errorf("registry exploded")
	withProvisioningStore(t, store)

	req := newRequest(http.MethodGet, "/api/provisioning/catalog", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningCatalog(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("failing store GetProvisioningCatalog: status = %d, want 502: %s", w.Code, w.Body.String())
	}
}
