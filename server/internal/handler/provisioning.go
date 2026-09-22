// Серверная часть внутрипериметрового реестра пакетов: чтение манифеста и
// блобов доступно любому участнику пространства, а чтение/запись pin-файла
// — только владельцу/админу. Разбор манифеста и резолвинг зависимостей
// живут в server/internal/provisioning; этот файл — HTTP-обвязка поверх
// них: разбор запроса, определение каталога вызывающего пространства и
// перевод его ошибок в нужные HTTP-статусы.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/provisioning"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type ProvisioningManifestResponse struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Packages      []provisioning.PackageManifest `json:"packages"`

	TotalBeforePlatformFilter int      `json:"totalBeforePlatformFilter"`
	PlatformsAvailable        []string `json:"platformsAvailable"`

	RevokedPackages []string `json:"revokedPackages"`

	UnavailablePackages []ProvisioningUnavailablePackage `json:"unavailablePackages"`
}

type ProvisioningUnavailablePackage struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

func provisioningUnavailableToResponse(in []provisioning.UnavailablePackage) []ProvisioningUnavailablePackage {
	out := make([]ProvisioningUnavailablePackage, len(in))
	for i, u := range in {
		out[i] = ProvisioningUnavailablePackage{Key: u.Key, Reason: u.Reason}
	}
	return out
}

type ProvisioningPinResponse struct {
	PackageName string  `json:"package_name"`
	PackageType string  `json:"package_type"`
	Version     string  `json:"version"`
	Enabled     bool    `json:"enabled"`
	UpdatedAt   string  `json:"updated_at"`
	UpdatedBy   *string `json:"updated_by,omitempty"`
}

type ProvisioningPinsResponse struct {
	Pins []ProvisioningPinResponse `json:"pins"`
}

type ProvisioningCatalogResponse struct {
	SchemaVersion int                            `json:"schemaVersion"`
	Packages      []provisioning.PackageManifest `json:"packages"`
}

type ProvisioningPinInput struct {
	PackageName string `json:"package_name"`
	PackageType string `json:"package_type"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

type PutProvisioningPinsRequest struct {
	Pins []ProvisioningPinInput `json:"pins"`
}

func provisioningPinToResponse(pin db.ProvisioningPin) ProvisioningPinResponse {
	resp := ProvisioningPinResponse{
		PackageName: pin.PackageName,
		PackageType: pin.PackageType,
		Version:     pin.Version,
		Enabled:     pin.Enabled,
		UpdatedAt:   pin.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if pin.UpdatedBy.Valid {
		s := uuidToString(pin.UpdatedBy)
		resp.UpdatedBy = &s
	}
	return resp
}

func provisioningPinsToResponse(pins []db.ProvisioningPin) []ProvisioningPinResponse {
	out := make([]ProvisioningPinResponse, len(pins))
	for i, pin := range pins {
		out[i] = provisioningPinToResponse(pin)
	}
	return out
}

func sortPackageManifests(manifests []provisioning.PackageManifest) {
	sort.Slice(manifests, func(i, j int) bool {
		a, b := manifests[i], manifests[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Version < b.Version
	})
}

func (h *Handler) resolveWorkspaceCatalog(ctx context.Context, workspaceID string) ([]provisioning.PackageManifest, []provisioning.UnavailablePackage, error) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid workspace id: %w", err)
	}

	allPins, err := h.Queries.ListProvisioningPins(ctx, wsUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("load provisioning pins: %w", err)
	}

	if len(allPins) == 0 {
		all, err := h.ProvisioningStore.List(ctx)
		if err != nil {
			if errors.Is(err, provisioning.ErrCatalogNotFound) {

				return nil, nil, nil
			}
			return nil, nil, fmt.Errorf("list provisioning catalog: %w", err)
		}
		return provisioning.ResolveRequiresPartial(ctx, all, h.ProvisioningStore.Manifest)
	}

	roots := make([]provisioning.PackageManifest, 0, len(allPins))
	var unavailable []provisioning.UnavailablePackage
	for _, pin := range allPins {
		if !pin.Enabled {
			continue
		}
		m, err := h.ProvisioningStore.Manifest(ctx, pin.PackageName, pin.Version)
		if err != nil {

			if errors.Is(err, provisioning.ErrPackageNotFound) {
				unavailable = append(unavailable, provisioning.UnavailablePackage{
					Key:    fmt.Sprintf("%s:%s@%s", pin.PackageType, pin.PackageName, pin.Version),
					Reason: fmt.Sprintf("package not found: %s@%s", pin.PackageName, pin.Version),
				})
				continue
			}
			return nil, nil, fmt.Errorf("pinned package %s:%s@%s: %w", pin.PackageType, pin.PackageName, pin.Version, err)
		}
		if m.Type != pin.PackageType {
			return nil, nil, fmt.Errorf("pinned package %s:%s@%s: manifest declares type %q",
				pin.PackageType, pin.PackageName, pin.Version, m.Type)
		}
		roots = append(roots, m)
	}

	resolved, moreUnavailable, err := provisioning.ResolveRequiresPartial(ctx, roots, h.ProvisioningStore.Manifest)
	if err != nil {
		return nil, nil, err
	}
	return resolved, append(unavailable, moreUnavailable...), nil
}

func (h *Handler) GetProvisioningManifest(w http.ResponseWriter, r *http.Request) {
	if h.ProvisioningStore == nil {
		writeError(w, http.StatusServiceUnavailable, "provisioning is not configured")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if !provisioning.ValidPlatform(platform) {
		writeError(w, http.StatusBadRequest, "invalid or missing platform query parameter")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	served, unavailable, err := h.resolveServedCatalog(r.Context(), workspaceID)
	if err != nil {
		slog.Error("provisioning: failed to resolve workspace manifest", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusBadGateway, "failed to resolve provisioning manifest")
		return
	}
	if len(unavailable) > 0 {
		slog.Warn("provisioning: manifest has unavailable packages", "workspace_id", workspaceID, "unavailable", unavailable)
	}

	h.recordDeliveredPackages(r.Context(), wsUUID, served)

	revoked, err := h.revokedPackagesForWorkspace(r.Context(), wsUUID, served)
	if err != nil {

		slog.Warn("provisioning: revoked-package diff failed", "workspace_id", workspaceID, "error", err)
		revoked = []string{}
	}

	filtered := provisioning.FilterByPlatform(served, platform)
	sortPackageManifests(filtered)

	writeJSON(w, http.StatusOK, ProvisioningManifestResponse{
		SchemaVersion:             provisioning.CurrentSchemaVersion,
		Packages:                  filtered,
		TotalBeforePlatformFilter: len(served),
		PlatformsAvailable:        provisioning.PlatformsOf(served),
		RevokedPackages:           revoked,
		UnavailablePackages:       provisioningUnavailableToResponse(unavailable),
	})
}

func (h *Handler) GetProvisioningBlob(w http.ResponseWriter, r *http.Request) {
	if h.ProvisioningStore == nil {
		writeError(w, http.StatusServiceUnavailable, "provisioning is not configured")
		return
	}
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	if name == "" || version == "" {
		writeError(w, http.StatusBadRequest, "name and version are required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	if !provisioning.ValidPlatform(platform) {
		writeError(w, http.StatusBadRequest, "invalid or missing platform query parameter")
		return
	}

	resolved, _, err := h.resolveServedCatalog(r.Context(), workspaceID)
	if err != nil {
		slog.Error("provisioning: failed to resolve workspace catalog for blob download", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusBadGateway, "failed to resolve provisioning manifest")
		return
	}

	var target *provisioning.PackageManifest
	for i := range resolved {
		if resolved[i].Name == name && resolved[i].Version == version && resolved[i].MatchesPlatform(platform) {
			target = &resolved[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "package not found")
		return
	}

	reader, size, err := h.ProvisioningStore.Blob(r.Context(), name, version)
	if err != nil {
		slog.Error("provisioning: failed to open package blob", "name", name, "version", version, "error", err)
		writeError(w, http.StatusNotFound, "package blob not found")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("X-Package-Sha256", target.SHA256)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, reader); err != nil {
		slog.Error("provisioning: failed to stream package blob", "name", name, "version", version, "error", err)
	}
}

func isWorkspaceOwnerOrAdmin(role string) bool {
	return role == "owner" || role == "admin"
}

func (h *Handler) requireProvisioningPinAdmin(w http.ResponseWriter, r *http.Request) (db.Member, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Member{}, "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Member{}, "", false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return db.Member{}, "", false
	}
	if !isWorkspaceOwnerOrAdmin(member.Role) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can manage provisioning pins")
		return db.Member{}, "", false
	}
	return member, workspaceID, true
}

func (h *Handler) GetProvisioningCatalog(w http.ResponseWriter, r *http.Request) {
	if h.ProvisioningStore == nil {
		writeError(w, http.StatusServiceUnavailable, "provisioning is not configured")
		return
	}
	if _, _, ok := h.requireProvisioningPinAdmin(w, r); !ok {
		return
	}
	all, err := h.ProvisioningStore.List(r.Context())
	if err != nil {
		if errors.Is(err, provisioning.ErrCatalogNotFound) {

			writeJSON(w, http.StatusOK, ProvisioningCatalogResponse{
				SchemaVersion: provisioning.CurrentSchemaVersion,
				Packages:      []provisioning.PackageManifest{},
			})
			return
		}
		slog.Error("provisioning: failed to list deployment catalog", "error", err)
		writeError(w, http.StatusBadGateway, "failed to list provisioning catalog")
		return
	}
	sortPackageManifests(all)
	if all == nil {
		all = []provisioning.PackageManifest{}
	}
	writeJSON(w, http.StatusOK, ProvisioningCatalogResponse{
		SchemaVersion: provisioning.CurrentSchemaVersion,
		Packages:      all,
	})
}

func (h *Handler) GetProvisioningPins(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireProvisioningPinAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	pins, err := h.Queries.ListProvisioningPins(r.Context(), wsUUID)
	if err != nil {
		slog.Error("provisioning: failed to list pins", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list provisioning pins")
		return
	}
	writeJSON(w, http.StatusOK, ProvisioningPinsResponse{Pins: provisioningPinsToResponse(pins)})
}

func (h *Handler) PutProvisioningPins(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.requireProvisioningPinAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var body PutProvisioningPinsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	seen := make(map[string]bool, len(body.Pins))
	for _, pin := range body.Pins {
		if !provisioning.ValidPackageType(pin.PackageType) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid package_type %q", pin.PackageType))
			return
		}
		if !provisioning.ValidPackageIdentifier(pin.PackageName) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid package_name %q", pin.PackageName))
			return
		}
		if !provisioning.ValidPackageIdentifier(pin.Version) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid version %q", pin.Version))
			return
		}
		key := pin.PackageType + ":" + pin.PackageName
		if seen[key] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("duplicate pin for %s", key))
			return
		}
		seen[key] = true
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("provisioning: failed to start pin update transaction", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update provisioning pins")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteProvisioningPinsForWorkspace(r.Context(), wsUUID); err != nil {
		slog.Error("provisioning: failed to clear pins", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update provisioning pins")
		return
	}

	saved := make([]db.ProvisioningPin, 0, len(body.Pins))
	for _, pin := range body.Pins {
		row, err := qtx.UpsertProvisioningPin(r.Context(), db.UpsertProvisioningPinParams{
			WorkspaceID: wsUUID,
			PackageName: pin.PackageName,
			PackageType: pin.PackageType,
			Version:     pin.Version,
			Enabled:     pin.Enabled,
			UpdatedBy:   member.UserID,
		})
		if err != nil {
			slog.Error("provisioning: failed to upsert pin", "workspace_id", workspaceID, "package_name", pin.PackageName, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update provisioning pins")
			return
		}
		saved = append(saved, row)
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("provisioning: failed to commit pin update", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update provisioning pins")
		return
	}

	writeJSON(w, http.StatusOK, ProvisioningPinsResponse{Pins: provisioningPinsToResponse(saved)})
}
