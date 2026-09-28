package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// --- provisioning_pins (не зависит от каталога) --------------------------------

func (d *Deps) handleGetProvisioningPins(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isOwnerOrAdmin(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "owner/admin required")
		return
	}
	pins, err := d.listPins(r, workspaceID)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, ProvisioningPinsList{Pins: pins})
}

func (d *Deps) listPins(r *http.Request, workspaceID string) ([]ProvisioningPin, error) {
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT prov_package_name, prov_package_type, prov_version, prov_enabled, updated_at, prov_updated_by
		FROM provisioning_pins WHERE workspace_id = $1 ORDER BY prov_package_type, prov_package_name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProvisioningPin{}
	for rows.Next() {
		var p ProvisioningPin
		if err := rows.Scan(&p.PackageName, &p.PackageType, &p.Version, &p.Enabled, &p.UpdatedAt, &p.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *Deps) isOwnerOrAdmin(r *http.Request, workspaceID, userID string) bool {
	role, ok, err := d.Workspace.HTTPAPIMembership().MemberRole(r.Context(), workspaceID, userID)
	return err == nil && ok && httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin)
}

func (d *Deps) handlePutProvisioningPins(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isOwnerOrAdmin(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "owner/admin required")
		return
	}
	var req PutPinsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	seen := map[string]bool{}
	for _, p := range req.Pins {
		if !validPackageType(p.PackageType) {
			httpapi.BadRequest(w, "invalid package_type")
			return
		}
		if !validPackageIdent(p.PackageName) || !validPackageIdent(p.Version) {
			httpapi.BadRequest(w, "invalid package_name/version")
			return
		}
		key := p.PackageType + "/" + p.PackageName
		if seen[key] {
			httpapi.BadRequest(w, "duplicate pin for "+key)
			return
		}
		seen[key] = true
	}
	err := d.DB.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM provisioning_pins WHERE workspace_id = $1`, workspaceID); err != nil {
			return err
		}
		for _, p := range req.Pins {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO provisioning_pins (workspace_id, prov_package_name, prov_package_type, prov_version, prov_enabled, prov_updated_by)
				VALUES ($1, $2, $3, $4, $5, $6)`, workspaceID, p.PackageName, p.PackageType, p.Version, p.Enabled, actor.UserID); err != nil {
				return err
			}
		}
		return nil
	})
	if checkErr(w, err) {
		return
	}
	pins, err := d.listPins(r, workspaceID)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, ProvisioningPinsList{Pins: pins})
}

// --- каталог/манифест/блоб (файловый бэкенд, см. server2/docs/decisions.md) ---

func (d *Deps) catalogPackages() ([]ProvisioningPackage, bool) {
	if d.Config.ProvisioningCatalogDir == "" {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(d.Config.ProvisioningCatalogDir, "catalog.json"))
	if err != nil {
		return nil, false
	}
	var cat ProvisioningCatalog
	if json.Unmarshal(raw, &cat) != nil {
		return nil, false
	}
	return cat.Packages, true
}

func (d *Deps) handleGetProvisioningCatalog(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isOwnerOrAdmin(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "owner/admin required")
		return
	}
	packages, ok := d.catalogPackages()
	if !ok {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "provisioning is not configured on this deployment", "provisioning_unavailable")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, ProvisioningCatalog{SchemaVersion: 1, Packages: packages})
}

func (d *Deps) handleGetProvisioningManifest(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isWorkspaceMember(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "not a member of this workspace")
		return
	}
	platform := r.URL.Query().Get("platform")
	if !validPlatforms[platform] {
		httpapi.BadRequest(w, "platform is required and must be a known value")
		return
	}
	all, ok := d.catalogPackages()
	if !ok {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "provisioning is not configured on this deployment", "provisioning_unavailable")
		return
	}
	pins, err := d.listPins(r, workspaceID)
	if checkErr(w, err) {
		return
	}

	base := all
	if len(pins) > 0 {
		pinned := map[string]bool{}
		for _, p := range pins {
			if p.Enabled {
				pinned[p.PackageType+"/"+p.PackageName+"@"+p.Version] = true
			}
		}
		base = filterPinned(all, pinned)
	}

	total := len(base)
	filtered := make([]ProvisioningPackage, 0, len(base))
	for _, p := range base {
		if p.Platform == "*" || p.Platform == platform {
			filtered = append(filtered, p)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, ProvisioningManifest{
		SchemaVersion: 1, Packages: filtered, TotalBeforePlatformFilter: total,
		PlatformsAvailable: []string{"*", "darwin-arm64", "darwin-x64", "win-x64", "linux-x64", "linux-arm64"},
		RevokedPackages:    []string{}, UnavailablePackages: []ProvisioningUnavailablePackage{},
	})
}

// filterPinned — манифест из закреплённых пакетов плюс их транзитивные
// requires (contract §9: "манифест строится из них плюс транзитивные requires").
func filterPinned(all []ProvisioningPackage, pinnedKeys map[string]bool) []ProvisioningPackage {
	byKey := map[string]ProvisioningPackage{}
	for _, p := range all {
		byKey[p.Type+"/"+p.Name+"@"+p.Version] = p
	}
	include := map[string]bool{}
	var visit func(key string)
	visit = func(key string) {
		if include[key] {
			return
		}
		p, has := byKey[key]
		if !has {
			return
		}
		include[key] = true
		for _, req := range p.Requires {
			visit(req)
		}
	}
	for k := range pinnedKeys {
		visit(k)
	}
	out := make([]ProvisioningPackage, 0, len(include))
	for k := range include {
		out = append(out, byKey[k])
	}
	return out
}

func (d *Deps) handleGetProvisioningBlob(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isWorkspaceMember(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "not a member of this workspace")
		return
	}
	platform := r.URL.Query().Get("platform")
	if !validPlatforms[platform] {
		httpapi.BadRequest(w, "platform is required and must be a known value")
		return
	}
	if d.Config.ProvisioningCatalogDir == "" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "provisioning is not configured on this deployment", "provisioning_unavailable")
		return
	}
	name, version := r.PathValue("name"), r.PathValue("version")
	all, ok := d.catalogPackages()
	if !ok {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "provisioning is not configured on this deployment", "provisioning_unavailable")
		return
	}
	var pkg *ProvisioningPackage
	for i := range all {
		if all[i].Name == name && all[i].Version == version && (all[i].Platform == "*" || all[i].Platform == platform) {
			pkg = &all[i]
			break
		}
	}
	if pkg == nil {
		httpapi.NotFound(w, "package is not part of the manifest allowed for this workspace/platform")
		return
	}
	path := filepath.Join(d.Config.ProvisioningCatalogDir, "blobs", name+"-"+version+".zst")
	data, err := os.ReadFile(path)
	if err != nil {
		httpapi.NotFound(w, "blob not found in storage")
		return
	}
	if pkg.SHA256 != "" {
		w.Header().Set("X-Package-Sha256", pkg.SHA256)
	} else {
		w.Header().Set("X-Package-Sha256", hex.EncodeToString(sha256Sum(data)))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/zstd")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
