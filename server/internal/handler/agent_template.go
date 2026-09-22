package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/agenttmpl"
	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/skillsources"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

var agentTemplates *agenttmpl.Registry

var vendoredTemplateSkills *agenttmpl.VendoredRegistry

func init() {
	reg, err := agenttmpl.Load()
	if err != nil {
		panic("agenttmpl: failed to load templates at startup: " + err.Error())
	}
	agentTemplates = reg
	vendored, err := agenttmpl.LoadVendored()
	if err != nil {
		panic("agenttmpl: failed to load vendored template skills at startup: " + err.Error())
	}
	vendoredTemplateSkills = vendored
}

type AgentTemplateSkillResponse struct {
	SourceURL         string `json:"source_url"`
	CachedName        string `json:"cached_name"`
	CachedDescription string `json:"cached_description"`
}

type AgentTemplateSummaryResponse struct {
	Slug        string                       `json:"slug"`
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Category    string                       `json:"category,omitempty"`
	Icon        string                       `json:"icon,omitempty"`
	Accent      string                       `json:"accent,omitempty"`
	Skills      []AgentTemplateSkillResponse `json:"skills"`
}

type AgentTemplateResponse struct {
	AgentTemplateSummaryResponse
	Instructions string `json:"instructions"`
}

func templateToSummary(t agenttmpl.Template) AgentTemplateSummaryResponse {
	skills := make([]AgentTemplateSkillResponse, 0, len(t.Skills))
	for _, s := range t.Skills {
		skills = append(skills, AgentTemplateSkillResponse{
			SourceURL:         s.SourceURL,
			CachedName:        s.CachedName,
			CachedDescription: s.CachedDescription,
		})
	}
	return AgentTemplateSummaryResponse{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Icon:        t.Icon,
		Accent:      t.Accent,
		Skills:      skills,
	}
}

func templateToDetail(t agenttmpl.Template) AgentTemplateResponse {
	return AgentTemplateResponse{
		AgentTemplateSummaryResponse: templateToSummary(t),
		Instructions:                 t.Instructions,
	}
}

func (h *Handler) ListAgentTemplates(w http.ResponseWriter, r *http.Request) {
	tmpls := agentTemplates.List()
	resp := make([]AgentTemplateSummaryResponse, 0, len(tmpls))
	for _, t := range tmpls {
		resp = append(resp, templateToSummary(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetAgentTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	t, ok := agentTemplates.Get(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, templateToDetail(t))
}

type CreateAgentFromTemplateRequest struct {
	TemplateSlug       string `json:"template_slug"`
	Name               string `json:"name"`
	RuntimeID          string `json:"runtime_id"`
	Model              string `json:"model,omitempty"`
	Visibility         string `json:"visibility,omitempty"`
	MaxConcurrentTasks int32  `json:"max_concurrent_tasks,omitempty"`

	PermissionMode    *string                    `json:"permission_mode,omitempty"`
	InvocationTargets []AgentInvocationTargetDTO `json:"invocation_targets,omitempty"`

	Description  *string `json:"description,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
	AvatarURL    *string `json:"avatar_url,omitempty"`

	ExtraSkillIDs []string `json:"extra_skill_ids,omitempty"`
}

type CreateAgentFromTemplateResponse struct {
	Agent            AgentResponse `json:"agent"`
	ImportedSkillIDs []string      `json:"imported_skill_ids"`
	ReusedSkillIDs   []string      `json:"reused_skill_ids"`
}

type fetchFailureResponse struct {
	Error      string   `json:"error"`
	FailedURLs []string `json:"failed_urls"`
}

func (h *Handler) CreateAgentFromTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateAgentFromTemplateRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 6
	}

	tmpl, found := agentTemplates.Get(req.TemplateSlug)
	if !found {
		writeError(w, http.StatusBadRequest, "template not found: "+req.TemplateSlug)
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	_, hasTargets := rawFields["invocation_targets"]
	legacyVis := req.Visibility
	perm, _, permErr := parsePermissionInput(wsUUID, req.PermissionMode, req.InvocationTargets, req.PermissionMode != nil, hasTargets, &legacyVis)
	if permErr != nil {
		writeError(w, http.StatusBadRequest, permErr.Error())
		return
	}

	slog.Info("agent-template create: request received",
		append(logger.RequestAttrs(r),
			"template_slug", tmpl.Slug,
			"workspace_id", workspaceID,
			"skill_url_count", len(tmpl.Skills),
		)...)

	preReused := make(map[int]db.Skill, len(tmpl.Skills))
	toFetchRefs := make([]agenttmpl.TemplateSkillRef, 0, len(tmpl.Skills))
	toFetchOrigIdx := make([]int, 0, len(tmpl.Skills))
	for i, ref := range tmpl.Skills {
		if ref.CachedName == "" {
			toFetchRefs = append(toFetchRefs, ref)
			toFetchOrigIdx = append(toFetchOrigIdx, i)
			continue
		}
		existing, err := h.Queries.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        ref.CachedName,
		})
		if err == nil {
			preReused[i] = existing
			slog.Info("agent-template create: pre-reuse hit (skipped fetch)",
				append(logger.RequestAttrs(r),
					"index", i,
					"cached_name", ref.CachedName,
					"existing_skill_id", uuidToString(existing.ID),
				)...)
			continue
		}
		toFetchRefs = append(toFetchRefs, ref)
		toFetchOrigIdx = append(toFetchOrigIdx, i)
	}

	fetchedByOrigIdx := make(map[int]*importedSkill, len(toFetchRefs))
	networkRefs := make([]agenttmpl.TemplateSkillRef, 0, len(toFetchRefs))
	networkOrigIdx := make([]int, 0, len(toFetchRefs))
	for j, ref := range toFetchRefs {
		if vendored, ok := vendoredTemplateSkills.ForURL(ref.SourceURL); ok {
			fetchedByOrigIdx[toFetchOrigIdx[j]] = importedSkillFromVendored(vendored)
			continue
		}
		networkRefs = append(networkRefs, ref)
		networkOrigIdx = append(networkOrigIdx, toFetchOrigIdx[j])
	}
	vendoredCount := len(toFetchRefs) - len(networkRefs)

	for _, ref := range networkRefs {
		source, _, derr := detectImportSource(ref.SourceURL)
		if derr != nil {
			continue
		}
		if src := skillSourceFor(source); !skillsources.EnabledFromEnv(src) {
			writeError(w, http.StatusForbidden, fmt.Sprintf(
				"template skill %s is not bundled with the server and %s", ref.SourceURL, skillsources.DisabledMessage(src)))
			return
		}
	}

	fetchStart := time.Now()
	var fetched []*importedSkill
	var failedURLs []string
	if len(networkRefs) > 0 {
		httpClient := newImportHTTPClient()
		fetchCtx, cancelFetch := context.WithTimeout(r.Context(), importFetchTimeout)
		defer cancelFetch()
		fetched, failedURLs = fetchTemplateSkillsParallel(fetchCtx, httpClient, networkRefs)
	}
	slog.Info("agent-template create: fetch phase done",
		append(logger.RequestAttrs(r),
			"template_slug", tmpl.Slug,
			"fetch_duration_ms", time.Since(fetchStart).Milliseconds(),
			"pre_reused_count", len(preReused),
			"vendored_count", vendoredCount,
			"fetched_count", len(networkRefs)-len(failedURLs),
			"fail_count", len(failedURLs),
			"failed_urls", failedURLs,
		)...)
	if len(failedURLs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, fetchFailureResponse{
			Error:      "one or more skill sources are unavailable",
			FailedURLs: failedURLs,
		})
		return
	}

	for j, imp := range fetched {
		fetchedByOrigIdx[networkOrigIdx[j]] = imp
	}

	creatorUUID := parseUUID(ownerID)
	isFirstAgent := false
	if existing, listErr := h.Queries.ListAgents(r.Context(), wsUUID); listErr == nil {
		isFirstAgent = len(existing) == 0
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tx: "+err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	importedIDs := make([]string, 0, len(tmpl.Skills))
	reusedIDs := make([]string, 0, len(tmpl.Skills))
	allSkillIDs := make([]pgtype.UUID, 0, len(tmpl.Skills))

	for i, ref := range tmpl.Skills {

		if existing, ok := preReused[i]; ok {
			allSkillIDs = append(allSkillIDs, existing.ID)
			reusedIDs = append(reusedIDs, uuidToString(existing.ID))
			continue
		}

		imp := fetchedByOrigIdx[i]
		if imp == nil {

			writeError(w, http.StatusInternalServerError, fmt.Sprintf("internal: missing fetch result for skill index %d", i))
			return
		}

		existing, err := qtx.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        imp.name,
		})
		if err == nil {
			slog.Info("agent-template create: reusing existing skill (frontmatter-name match, cached_name drifted)",
				append(logger.RequestAttrs(r),
					"index", i,
					"frontmatter_name", imp.name,
					"cached_name", ref.CachedName,
					"existing_skill_id", uuidToString(existing.ID),
				)...)
			allSkillIDs = append(allSkillIDs, existing.ID)
			reusedIDs = append(reusedIDs, uuidToString(existing.ID))
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("agent-template create: lookup existing skill failed",
				append(logger.RequestAttrs(r),
					"index", i,
					"name", imp.name,
					"error", err,
				)...)
			writeError(w, http.StatusInternalServerError, "lookup existing skill failed: "+err.Error())
			return
		}

		slog.Info("agent-template create: inserting new skill",
			append(logger.RequestAttrs(r),
				"index", i,
				"name", imp.name,
				"file_count", len(imp.files),
			)...)

		files := make([]CreateSkillFileRequest, 0, len(imp.files))
		for _, f := range imp.files {
			if !validateFilePath(f.path) {
				continue
			}
			files = append(files, CreateSkillFileRequest{Path: f.path, Content: f.content})
		}

		origin := map[string]any{
			"type":          "agent_template",
			"template_slug": tmpl.Slug,
			"source_url":    ref.SourceURL,
		}

		if imp.origin != nil {
			for k, v := range imp.origin {
				if _, exists := origin[k]; !exists {
					origin[k] = v
				}
			}
		}

		created, err := createSkillWithFilesInTx(r.Context(), qtx, skillCreateInput{
			WorkspaceID: wsUUID,
			CreatorID:   creatorUUID,
			Name:        imp.name,
			Description: imp.description,
			Content:     imp.content,
			Config:      map[string]any{"origin": origin},
			Files:       files,
		})
		if err != nil {

			slog.Error("agent-template create: failed to create skill",
				append(logger.RequestAttrs(r),
					"index", i,
					"name", imp.name,
					"workspace_id", workspaceID,
					"error", err,
					"is_unique_violation", isUniqueViolation(err),
				)...)
			writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
			return
		}
		allSkillIDs = append(allSkillIDs, parseUUID(created.ID))
		importedIDs = append(importedIDs, created.ID)
	}

	rc, _ := json.Marshal(map[string]any{})
	ce, _ := json.Marshal(map[string]string{})
	ca, _ := json.Marshal([]string{})

	description := tmpl.Description
	if req.Description != nil {
		description = *req.Description
	}
	instructions := tmpl.Instructions
	if req.Instructions != nil {
		instructions = *req.Instructions
	}
	avatarURL := newAgentAvatar(req.AvatarURL)

	agent, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        wsUUID,
		Name:               req.Name,
		Description:        description,
		Instructions:       instructions,
		AvatarUrl:          avatarURL,
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeConfig:      rc,
		RuntimeID:          runtime.ID,
		Visibility:         perm.legacyVisibility(),
		PermissionMode:     perm.mode,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		OwnerID:            creatorUUID,
		CustomEnv:          ce,
		CustomArgs:         ca,
		McpConfig:          nil,
		Model:              pgtype.Text{String: req.Model, Valid: req.Model != ""},
	})
	if err != nil {

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			slog.Info("agent-template create: agent name conflict",
				append(logger.RequestAttrs(r),
					"agent_name", req.Name,
					"workspace_id", workspaceID,
				)...)
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", req.Name))
			return
		}
		slog.Error("agent-template create: failed to create agent",
			append(logger.RequestAttrs(r),
				"agent_name", req.Name,
				"workspace_id", workspaceID,
				"error", err,
				"is_unique_violation", isUniqueViolation(err),
			)...)
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}

	for idx, skillID := range allSkillIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: skillID,
		}); err != nil {
			slog.Error("agent-template create: failed to attach skill",
				append(logger.RequestAttrs(r),
					"agent_id", uuidToString(agent.ID),
					"skill_id", uuidToString(skillID),
					"skill_index", idx,
					"error", err,
				)...)
			writeError(w, http.StatusInternalServerError, "failed to attach skill: "+err.Error())
			return
		}
	}

	if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, agent.ID, creatorUUID, perm.targets); err != nil {
		slog.Error("agent-template create: persist invocation targets failed",
			append(logger.RequestAttrs(r),
				"agent_id", uuidToString(agent.ID),
				"error", err,
			)...)
		writeError(w, http.StatusInternalServerError, "failed to persist invocation targets: "+err.Error())
		return
	}

	for _, raw := range req.ExtraSkillIDs {
		extraUUID, perr := util.ParseUUID(raw)
		if perr != nil {

			slog.Warn("agent-template create: skipping malformed extra_skill_id",
				append(logger.RequestAttrs(r), "raw", raw, "error", perr)...)
			continue
		}

		owned, qerr := qtx.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID: extraUUID, WorkspaceID: wsUUID,
		})
		if qerr != nil {
			slog.Warn("agent-template create: skipping cross-workspace extra_skill_id",
				append(logger.RequestAttrs(r), "skill_id", raw, "error", qerr)...)
			continue
		}
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: owned.ID,
		}); err != nil {
			slog.Error("agent-template create: failed to attach extra skill",
				append(logger.RequestAttrs(r), "skill_id", raw, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to attach skill: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent-template create: commit failed",
			append(logger.RequestAttrs(r),
				"agent_id", uuidToString(agent.ID),
				"error", err,
			)...)
		writeError(w, http.StatusInternalServerError, "commit failed: "+err.Error())
		return
	}

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), agent.ID)
		agent, _ = h.Queries.GetAgent(r.Context(), agent.ID)
	}

	resp := h.agentToResponse(agent)

	if err := h.attachAgentSkills(r.Context(), &resp, agent.ID); err != nil {
		slog.Warn("load agent skills after template create failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}

	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, agent.ID); err != nil {
		slog.Warn("agent-template create: load invocation targets for response failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
	}
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)

	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(agent.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		tmpl.Slug,
		isFirstAgent,
	))

	slog.Info("agent created from template",
		append(logger.RequestAttrs(r),
			"agent_id", uuidToString(agent.ID),
			"template_slug", tmpl.Slug,
			"imported_skill_count", len(importedIDs),
			"reused_skill_count", len(reusedIDs),
		)...)

	redactAgentResponseForCaller(&resp, actorType, agent, ownerID,
		h.workspaceKioskRedactsMcpConfig(r.Context(), agent.WorkspaceID),
		h.composioMCPAppsEnabled(r.Context()))

	writeJSON(w, http.StatusCreated, CreateAgentFromTemplateResponse{
		Agent:            resp,
		ImportedSkillIDs: importedIDs,
		ReusedSkillIDs:   reusedIDs,
	})
}

type templateFetchResult struct {
	index    int
	imported *importedSkill
	url      string
	err      error
}

func fetchTemplateSkillsParallel(ctx context.Context, client *http.Client, refs []agenttmpl.TemplateSkillRef) ([]*importedSkill, []string) {
	results := make(chan templateFetchResult, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func(i int, ref agenttmpl.TemplateSkillRef) {
			defer wg.Done()
			start := time.Now()
			slog.Info("agent-template fetch: start", "index", i, "source_url", ref.SourceURL)
			imp, err := fetchSkillFromURL(ctx, client, ref.SourceURL)
			elapsedMs := time.Since(start).Milliseconds()
			if err != nil {
				slog.Warn("agent-template fetch: failed",
					"index", i,
					"source_url", ref.SourceURL,
					"duration_ms", elapsedMs,
					"error", err,
				)
			} else {
				resolvedName := ""
				fileCount := 0
				if imp != nil {
					resolvedName = imp.name
					fileCount = len(imp.files)
				}
				slog.Info("agent-template fetch: done",
					"index", i,
					"source_url", ref.SourceURL,
					"duration_ms", elapsedMs,
					"resolved_name", resolvedName,
					"file_count", fileCount,
				)
			}
			results <- templateFetchResult{index: i, imported: imp, url: ref.SourceURL, err: err}
		}(i, ref)
	}
	wg.Wait()
	close(results)

	imports := make([]*importedSkill, len(refs))
	var failed []string
	for r := range results {
		if r.err != nil {
			failed = append(failed, r.url)
			continue
		}
		imports[r.index] = r.imported
	}
	return imports, failed
}

func importedSkillFromVendored(v *agenttmpl.VendoredSkill) *importedSkill {
	files := make([]importedFile, 0, len(v.Files))
	for _, f := range v.Files {
		files = append(files, importedFile{path: f.Path, content: f.Content})
	}
	ref := v.Commit
	if ref == "" {
		ref = v.Ref
	}
	return &importedSkill{
		name:        v.Name,
		description: v.Description,
		content:     v.Content,
		files:       files,
		origin: map[string]any{
			"type":       "github",
			"source_url": v.SourceURL,
			"owner":      v.Owner,
			"repo":       v.Repo,
			"ref":        ref,
			"path":       v.Path,
			"vendored":   true,
		},
	}
}

func fetchSkillFromURL(ctx context.Context, client *http.Client, rawURL string) (*importedSkill, error) {
	source, normalized, err := detectImportSource(rawURL)
	if err != nil {
		return nil, err
	}

	if src := skillSourceFor(source); !skillsources.EnabledFromEnv(src) {
		return nil, fmt.Errorf("%s", skillsources.DisabledMessage(src))
	}
	switch source {
	case sourceClawHub:
		return fetchFromClawHub(ctx, client, normalized)
	case sourceSkillsSh:
		return fetchFromSkillsSh(ctx, client, normalized)
	case sourceGitHub:
		return fetchFromGitHub(ctx, client, normalized)
	}
	return nil, fmt.Errorf("unknown import source for %s", rawURL)
}
