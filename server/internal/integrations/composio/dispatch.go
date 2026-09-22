package composio

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/runtimeapps"
	"github.com/adanman/goosar/server/internal/util"
	sdk "github.com/adanman/goosar/server/pkg/composio"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const mcpOverlayServerName = "composio"

type composioMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type mcpOverlayPayload struct {
	MCPServers map[string]composioMCPServer `json:"mcpServers"`
}

func (s *Service) BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {

	if !agent.OwnerID.Valid {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	ownerUserID := agent.OwnerID

	allowSet := normaliseAllowlistToSet(agent.ComposioToolkitAllowlist)
	if len(allowSet) == 0 {
		return runtimeapps.MCPOverlayResult{}, nil
	}

	rows, err := s.store.ListActiveUserComposioConnections(ctx, ownerUserID)
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: build task overlay: list connections: %w", err)
	}
	pinned := pinConnectedAccounts(rows, allowSet)
	if len(pinned) == 0 {

		return runtimeapps.MCPOverlayResult{}, nil
	}

	slugs := make([]string, 0, len(pinned))
	for slug := range pinned {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	resp, err := s.sdk.CreateSession(ctx, sdk.CreateSessionRequest{
		UserID:            util.UUIDToString(ownerUserID),
		Toolkits:          map[string]any{"enable": slugs},
		ConnectedAccounts: pinned,
	})
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: build task overlay: create session: %w", err)
	}

	if resp == nil || resp.MCP.URL == "" {
		return runtimeapps.MCPOverlayResult{}, nil
	}

	payload := mcpOverlayPayload{
		MCPServers: map[string]composioMCPServer{
			mcpOverlayServerName: {
				Type:    "http",
				URL:     resp.MCP.URL,
				Headers: s.sdk.MCPAuthHeaders(),
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: marshal task overlay: %w", err)
	}
	apps := make([]runtimeapps.ConnectedApp, 0, len(slugs))
	for _, slug := range slugs {
		apps = append(apps, runtimeapps.ConnectedApp{
			Provider:    "composio",
			ServerName:  mcpOverlayServerName,
			ToolkitSlug: slug,
			ToolkitName: runtimeapps.DisplayNameForToolkitSlug(slug),
		})
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: raw, ConnectedApps: apps}, nil
}

func normaliseAllowlistToSet(allow []string) map[string]struct{} {
	if len(allow) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		slug := lowerTrim(s)
		if slug == "" {
			continue
		}
		out[slug] = struct{}{}
	}
	return out
}

func pinConnectedAccounts(rows []db.UserComposioConnection, allowSet map[string]struct{}) map[string]any {
	pinned := make(map[string]any, len(rows))
	for _, row := range rows {
		slug := lowerTrim(row.ToolkitSlug)
		if slug == "" {
			continue
		}
		if _, allowed := allowSet[slug]; !allowed {
			continue
		}
		if _, dup := pinned[slug]; dup {
			continue
		}
		pinned[slug] = []string{row.ConnectedAccountID}
	}
	return pinned
}

func lowerTrim(s string) string {

	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	if start == end {
		return ""
	}

	upper := false
	for i := start; i < end; i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			upper = true
			break
		}
	}
	if !upper {
		return s[start:end]
	}
	b := make([]byte, end-start)
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i-start] = c
	}
	return string(b)
}
