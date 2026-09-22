package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adanman/goosar/server/internal/provisioning"
)

func clearDeliveredPackages(t *testing.T) {
	t.Helper()
	wipe := func() {
		testPool.Exec(context.Background(),
			`DELETE FROM provisioning_delivered_package WHERE workspace_id = $1`, testWorkspaceID)
	}
	wipe()
	t.Cleanup(wipe)
}

func getManifest(t *testing.T) ProvisioningManifestResponse {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200: %s", w.Code, w.Body.String())
	}
	return decodeManifestResponse(t, w.Body)
}

func TestGetProvisioningManifest_RevokesPackageDroppedFromPins(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)

	store := newFakeProvisioningStore()
	keep, keepBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*")
	drop, dropBlob := fixturePackage(provisioning.PackageTypeSkill, "office-xlsx", "2.1.0", "*")
	store.add(keep, keepBlob)
	store.add(drop, dropBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, keep.Name, keep.Type, keep.Version, true)
	insertProvisioningPinFixture(t, testWorkspaceID, drop.Name, drop.Type, drop.Version, true)

	resp := getManifest(t)
	if !containsName(manifestPackageNames(resp), drop.Name) {
		t.Fatalf("first manifest missing %q: %v", drop.Name, manifestPackageNames(resp))
	}
	if len(resp.RevokedPackages) != 0 {
		t.Fatalf("first manifest revoked = %v, want none", resp.RevokedPackages)
	}

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM provisioning_pin WHERE workspace_id = $1 AND package_name = $2`,
		testWorkspaceID, drop.Name); err != nil {
		t.Fatalf("drop pin: %v", err)
	}

	resp = getManifest(t)
	names := manifestPackageNames(resp)
	if containsName(names, drop.Name) {
		t.Fatalf("dropped package still served: %v", names)
	}
	if !containsName(names, keep.Name) {
		t.Fatalf("kept package missing: %v", names)
	}
	want := "skill:office-xlsx@2.1.0"
	if !containsString(resp.RevokedPackages, want) {
		t.Fatalf("revoked = %v, want %q", resp.RevokedPackages, want)
	}
	if containsString(resp.RevokedPackages, "skill:office-docx@1.4.0") {
		t.Fatalf("still-pinned package must not be revoked: %v", resp.RevokedPackages)
	}

	resp = getManifest(t)
	if !containsString(resp.RevokedPackages, want) {
		t.Fatalf("revocation did not persist across contacts: %v", resp.RevokedPackages)
	}
}

func TestGetProvisioningManifest_MCPKillSwitchRevokesMcpPackages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)

	store := newFakeProvisioningStore()
	mcp, mcpBlob := fixturePackage(provisioning.PackageTypeMCPServer, "outlook", "1.2.0", "*")
	skill, skillBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*")
	store.add(mcp, mcpBlob)
	store.add(skill, skillBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, mcp.Name, mcp.Type, mcp.Version, true)
	insertProvisioningPinFixture(t, testWorkspaceID, skill.Name, skill.Type, skill.Version, true)

	resp := getManifest(t)
	if !containsName(manifestPackageNames(resp), mcp.Name) {
		t.Fatalf("mcp package not served before the kill switch: %v", manifestPackageNames(resp))
	}

	seedDeploymentPolicy(t, `{"mcp":{"*":{"enabled":false,"locked":true}}}`)

	resp = getManifest(t)
	names := manifestPackageNames(resp)
	if containsName(names, mcp.Name) {
		t.Fatalf("kill switch active but mcp package still served: %v", names)
	}
	if !containsName(names, skill.Name) {
		t.Fatalf("kill switch must not touch non-mcp packages: %v", names)
	}
	if !containsString(resp.RevokedPackages, "mcp-server:outlook@1.2.0") {
		t.Fatalf("revoked = %v, want the delivered mcp package", resp.RevokedPackages)
	}
	if containsString(resp.RevokedPackages, "skill:office-docx@1.4.0") {
		t.Fatalf("skill wrongly revoked by the MCP kill switch: %v", resp.RevokedPackages)
	}

	blobReq := withProvisioningBlobParams(
		newRequest(http.MethodGet, "/api/provisioning/blob/outlook/1.2.0?platform=darwin-arm64", nil),
		mcp.Name, mcp.Version)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningBlob(w, blobReq)
	if w.Code != http.StatusNotFound {
		t.Fatalf("blob under kill switch: status = %d, want 404", w.Code)
	}
}

func TestGetProvisioningManifest_RepinnedPackageIsServedAgain(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)

	store := newFakeProvisioningStore()
	pkg, blob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*")
	store.add(pkg, blob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, pkg.Name, pkg.Type, pkg.Version, true)
	getManifest(t)

	insertProvisioningPinFixture(t, testWorkspaceID, pkg.Name, pkg.Type, pkg.Version, false)
	resp := getManifest(t)
	if len(resp.Packages) != 0 {
		t.Fatalf("disabled pin still serves packages: %v", manifestPackageNames(resp))
	}
	if !containsString(resp.RevokedPackages, "skill:office-docx@1.4.0") {
		t.Fatalf("revoked = %v, want the delivered skill", resp.RevokedPackages)
	}

	insertProvisioningPinFixture(t, testWorkspaceID, pkg.Name, pkg.Type, pkg.Version, true)
	resp = getManifest(t)
	if !containsName(manifestPackageNames(resp), pkg.Name) {
		t.Fatalf("re-pinned package not served: %v", manifestPackageNames(resp))
	}
	if len(resp.RevokedPackages) != 0 {
		t.Fatalf("re-pinned package still revoked: %v", resp.RevokedPackages)
	}
}

func TestResolveEffectiveConfig_RevokedPackagesFollowDeliveredDiff(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)
	ctx := context.Background()

	store := newFakeProvisioningStore()
	keep, keepBlob := fixturePackage(provisioning.PackageTypeSkill, "office-docx", "1.4.0", "*")
	store.add(keep, keepBlob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, keep.Name, keep.Type, keep.Version, true)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO provisioning_delivered_package (workspace_id, package_name, package_type, version)
		VALUES ($1, 'office-xlsx', 'skill', '2.1.0'), ($1, 'office-docx', 'skill', '1.4.0')
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed delivered memory: %v", err)
	}

	eff, err := testHandler.ResolveEffectiveConfig(ctx, parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !containsString(eff.RevokedPackages, "skill:office-xlsx@2.1.0") {
		t.Fatalf("revoked_packages = %v, want the dropped skill", eff.RevokedPackages)
	}
	if containsString(eff.RevokedPackages, "skill:office-docx@1.4.0") {
		t.Fatalf("still-pinned package revoked in effective config: %v", eff.RevokedPackages)
	}
}

func TestResolveEffectiveConfig_RevokedPackagesIncludeMCPKillSwitch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)
	ctx := context.Background()

	store := newFakeProvisioningStore()
	mcp, mcpBlob := fixturePackage(provisioning.PackageTypeMCPServer, "outlook", "1.2.0", "*")
	store.add(mcp, mcpBlob)
	withProvisioningStore(t, store)
	insertProvisioningPinFixture(t, testWorkspaceID, mcp.Name, mcp.Type, mcp.Version, true)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO provisioning_delivered_package (workspace_id, package_name, package_type, version)
		VALUES ($1, 'outlook', 'mcp-server', '1.2.0')
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed delivered memory: %v", err)
	}

	seedDeploymentPolicy(t, `{"mcp":{"*":{"enabled":false,"locked":true}}}`)

	eff, err := testHandler.ResolveEffectiveConfig(ctx, parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !containsString(eff.RevokedPackages, "mcp-server:outlook@1.2.0") {
		t.Fatalf("revoked_packages = %v, want the delivered mcp package under the kill switch", eff.RevokedPackages)
	}
}

func TestResolveEffectiveConfig_RevokedPackagesFailSafe(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO provisioning_delivered_package (workspace_id, package_name, package_type, version)
		VALUES ($1, 'office-xlsx', 'skill', '2.1.0')
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed delivered memory: %v", err)
	}

	withProvisioningStore(t, nil)
	eff, err := testHandler.ResolveEffectiveConfig(ctx, parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("resolve without store: %v", err)
	}
	if len(eff.RevokedPackages) != 0 {
		t.Fatalf("revoked_packages = %v, want empty when provisioning is unconfigured", eff.RevokedPackages)
	}

	broken := newFakeProvisioningStore()
	broken.listErr = context.DeadlineExceeded
	withProvisioningStore(t, broken)
	eff, err = testHandler.ResolveEffectiveConfig(ctx, parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("resolve with broken store: %v", err)
	}
	if len(eff.RevokedPackages) != 0 {
		t.Fatalf("revoked_packages = %v, want empty on catalog failure", eff.RevokedPackages)
	}
}

func TestGetProvisioningManifest_StoreReadFailureIsNotMassRevocation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`DELETE FROM provisioning_pin WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("clear pins: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO provisioning_delivered_package (workspace_id, package_name, package_type, version)
		VALUES ($1, 'office-xlsx', 'skill', '2.1.0')
	`, testWorkspaceID); err != nil {
		t.Fatalf("seed delivered memory: %v", err)
	}

	broken := newFakeProvisioningStore()
	broken.listErr = fmt.Errorf("provisioning: read catalog: open catalog.json: permission denied")
	withProvisioningStore(t, broken)

	req := newRequest(http.MethodGet, "/api/provisioning/manifest?platform=darwin-arm64", nil)
	w := httptest.NewRecorder()
	testHandler.GetProvisioningManifest(w, req)
	if w.Code < 500 || w.Code >= 600 {
		t.Fatalf("manifest status = %d, want 5xx on store read failure: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM provisioning_delivered_package WHERE workspace_id = $1`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count delivered: %v", err)
	}
	if count != 1 {
		t.Fatalf("delivered memory rows = %d, want 1 (untouched)", count)
	}
}

func TestGetProvisioningManifest_StagedUpgradeRecordsBothDeliveredVersions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	clearDeliveredPackages(t)

	store := newFakeProvisioningStore()
	sharedOld, sharedOldBlob := fixturePackage(provisioning.PackageTypeSkill, "stage-shared", "1.0.0", "*")
	sharedNew, sharedNewBlob := fixturePackage(provisioning.PackageTypeSkill, "stage-shared", "2.0.0", "*")
	root, rootBlob := fixturePackage(provisioning.PackageTypeSkill, "stage-root", "1.0.0", "*", "skill:stage-shared@1.0.0")
	store.add(sharedOld, sharedOldBlob)
	store.add(sharedNew, sharedNewBlob)
	store.add(root, rootBlob)
	withProvisioningStore(t, store)

	insertProvisioningPinFixture(t, testWorkspaceID, root.Name, root.Type, root.Version, true)
	insertProvisioningPinFixture(t, testWorkspaceID, sharedNew.Name, sharedNew.Type, sharedNew.Version, true)

	resp := getManifest(t)
	if len(resp.RevokedPackages) != 0 {
		t.Fatalf("staged upgrade must not revoke anything: %v", resp.RevokedPackages)
	}

	rows, err := testPool.Query(context.Background(), `
		SELECT package_name, version FROM provisioning_delivered_package
		WHERE workspace_id = $1 ORDER BY package_name, version
	`, testWorkspaceID)
	if err != nil {
		t.Fatalf("query delivered memory: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name, version string
		if err := rows.Scan(&name, &version); err != nil {
			t.Fatalf("scan delivered row: %v", err)
		}
		got = append(got, name+"@"+version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate delivered rows: %v", err)
	}
	want := []string{"stage-root@1.0.0", "stage-shared@1.0.0", "stage-shared@2.0.0"}
	if len(got) != len(want) {
		t.Fatalf("delivered memory = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivered memory = %v, want %v", got, want)
		}
	}

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM provisioning_pin WHERE workspace_id = $1 AND package_name = $2`,
		testWorkspaceID, root.Name); err != nil {
		t.Fatalf("drop root pin: %v", err)
	}
	resp = getManifest(t)
	if !containsString(resp.RevokedPackages, "skill:stage-root@1.0.0") {
		t.Fatalf("revoked = %v, want the unpinned root package", resp.RevokedPackages)
	}
	if !containsString(resp.RevokedPackages, "skill:stage-shared@1.0.0") {
		t.Fatalf("revoked = %v, want the no-longer-served old version", resp.RevokedPackages)
	}
	if containsString(resp.RevokedPackages, "skill:stage-shared@2.0.0") {
		t.Fatalf("still-served version wrongly revoked: %v", resp.RevokedPackages)
	}
}
