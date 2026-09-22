// Отзыв пакетов как состояние, а не событие: ручка манифеста фиксирует, что
// было отдано, и на каждый запрос пересчитывает revoked = выданное минус
// то, что отдаётся сейчас, — машина, ушедшая в оффлайн, узнаёт об отзыве
// при первом же обращении. Набор сужают снятый пин администратора
// пространства и общий выключатель MCP в политике деплоя. Тот же список
// едет и в effective_config.revoked_packages, но фиксирует выдачи только
// ручка манифеста. Если каталог не резолвится, список отзыва пуст —
// неопределённость не должна читаться как «отозвать всё».
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/provisioning"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func revokedPackageLabel(pkgType, name, version string) string {
	return pkgType + ":" + name + "@" + version
}

func (h *Handler) deploymentMCPKillSwitchActive(ctx context.Context) (bool, error) {
	row, err := h.Queries.GetDeploymentPolicy(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load deployment policy: %w", err)
	}
	var doc deploymentPolicyDoc
	if len(row.Policy) > 0 {
		if err := json.Unmarshal(row.Policy, &doc); err != nil {
			return false, fmt.Errorf("parse deployment policy: %w", err)
		}
	}
	_, active := doc.MCP[mcpPolicyWildcard]
	return active, nil
}

func (h *Handler) resolveServedCatalog(ctx context.Context, workspaceID string) ([]provisioning.PackageManifest, []provisioning.UnavailablePackage, error) {
	resolved, unavailable, err := h.resolveWorkspaceCatalog(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	killSwitch, err := h.deploymentMCPKillSwitchActive(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !killSwitch {
		return resolved, unavailable, nil
	}
	served := make([]provisioning.PackageManifest, 0, len(resolved))
	for _, m := range resolved {
		if m.Type == provisioning.PackageTypeMCPServer {
			continue
		}
		served = append(served, m)
	}
	return served, unavailable, nil
}

func (h *Handler) recordDeliveredPackages(ctx context.Context, workspaceID pgtype.UUID, served []provisioning.PackageManifest) {
	if len(served) == 0 {
		return
	}
	names := make([]string, len(served))
	types := make([]string, len(served))
	versions := make([]string, len(served))
	for i, m := range served {
		names[i] = m.Name
		types[i] = m.Type
		versions[i] = m.Version
	}
	if err := h.Queries.UpsertDeliveredProvisioningPackages(ctx, db.UpsertDeliveredProvisioningPackagesParams{
		WorkspaceID:  workspaceID,
		PackageNames: names,
		PackageTypes: types,
		Versions:     versions,
	}); err != nil {
		slog.Warn("provisioning: failed to record delivered packages",
			"workspace_id", uuidToString(workspaceID), "error", err)
	}
}

func (h *Handler) revokedPackagesForWorkspace(ctx context.Context, workspaceID pgtype.UUID, served []provisioning.PackageManifest) ([]string, error) {
	delivered, err := h.Queries.ListDeliveredProvisioningPackages(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list delivered packages: %w", err)
	}
	servedKeys := make(map[string]struct{}, len(served))
	for _, m := range served {
		servedKeys[revokedPackageLabel(m.Type, m.Name, m.Version)] = struct{}{}
	}
	revoked := []string{}
	for _, row := range delivered {
		label := revokedPackageLabel(row.PackageType, row.PackageName, row.Version)
		if _, still := servedKeys[label]; still {
			continue
		}
		revoked = append(revoked, label)
	}
	sort.Strings(revoked)
	return revoked, nil
}

func (h *Handler) revokedPackagesForEffectiveConfig(ctx context.Context, workspaceID pgtype.UUID) []string {
	if h.ProvisioningStore == nil {
		return []string{}
	}
	served, _, err := h.resolveServedCatalog(ctx, uuidToString(workspaceID))
	if err != nil {
		slog.Warn("effective config: catalog resolution failed; serving empty revoked_packages",
			"workspace_id", uuidToString(workspaceID), "error", err)
		return []string{}
	}
	revoked, err := h.revokedPackagesForWorkspace(ctx, workspaceID, served)
	if err != nil {
		slog.Warn("effective config: revoked-package diff failed; serving empty revoked_packages",
			"workspace_id", uuidToString(workspaceID), "error", err)
		return []string{}
	}
	return revoked
}
