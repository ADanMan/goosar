package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	skillpkg "github.com/adanman/goosar/server/internal/skill"
	"github.com/adanman/goosar/server/internal/skillsources"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func sanitizeNullBytes(s string) string {
	return util.SanitizeTextForPostgres(s)
}

type SkillResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	Config      any     `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type SkillSummaryResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Config      any     `json:"config"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`

	Enabled *bool `json:"enabled,omitempty"`
}

type AgentSkillSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type SkillFileResponse struct {
	ID        string `json:"id"`
	SkillID   string `json:"skill_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type SkillSearchCandidateResponse struct {
	Name         string  `json:"name"`
	URL          string  `json:"url"`
	Source       string  `json:"source"`
	Repo         *string `json:"repo"`
	InstallCount *int64  `json:"install_count"`
	GitHubStars  *int64  `json:"github_stars"`
	Description  string  `json:"description"`
}

type SkillWithFilesResponse struct {
	SkillResponse
	Files []SkillFileResponse `json:"files"`
}

type SkillImportResult struct {
	Status        string                  `json:"status"`
	Reason        string                  `json:"reason,omitempty"`
	Skill         *SkillWithFilesResponse `json:"skill,omitempty"`
	ExistingSkill *ExistingSkillIdentity  `json:"existing_skill,omitempty"`
}

type ExistingSkillIdentity struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreatedBy    string `json:"created_by,omitempty"`
	CanOverwrite bool   `json:"can_overwrite,omitempty"`
}

func writeSkillImportDuplicateConflict(w http.ResponseWriter, existing ExistingSkillIdentity) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":          "a skill with this name already exists",
		"existing_skill": existing,
	})
}

func skillToResponse(s db.Skill) SkillResponse {
	return SkillResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Name:        s.Name,
		Description: s.Description,
		Content:     s.Content,
		Config:      decodeSkillConfig(s.Config),
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func (h *Handler) existingSkillIdentityByName(ctx context.Context, workspaceID pgtype.UUID, name string) (ExistingSkillIdentity, bool, error) {
	skill, err := h.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ExistingSkillIdentity{}, false, nil
		}
		return ExistingSkillIdentity{}, false, err
	}
	return existingSkillIdentity(skill, ""), true, nil
}

func existingSkillIdentity(skill db.Skill, userID string) ExistingSkillIdentity {
	identity := ExistingSkillIdentity{
		ID:           uuidToString(skill.ID),
		Name:         skill.Name,
		CanOverwrite: canOverwriteSkillByLocalImport(userID, skill),
	}
	if skill.CreatedBy.Valid {
		identity.CreatedBy = uuidToString(skill.CreatedBy)
	}
	return identity
}

func decodeSkillConfig(raw []byte) any {
	var config any
	if raw != nil {
		_ = json.Unmarshal(raw, &config)
	}
	if config == nil {
		return map[string]any{}
	}
	return config
}

func skillSummaryToResponse(
	id, workspaceID pgtype.UUID,
	name, description string,
	config []byte,
	createdBy pgtype.UUID,
	createdAt, updatedAt pgtype.Timestamptz,
) SkillSummaryResponse {
	return SkillSummaryResponse{
		ID:          uuidToString(id),
		WorkspaceID: uuidToString(workspaceID),
		Name:        name,
		Description: description,
		Config:      decodeSkillConfig(config),
		CreatedBy:   uuidToPtr(createdBy),
		CreatedAt:   timestampToString(createdAt),
		UpdatedAt:   timestampToString(updatedAt),
	}
}

func skillFileToResponse(f db.SkillFile) SkillFileResponse {
	return SkillFileResponse{
		ID:        uuidToString(f.ID),
		SkillID:   uuidToString(f.SkillID),
		Path:      f.Path,
		Content:   f.Content,
		CreatedAt: timestampToString(f.CreatedAt),
		UpdatedAt: timestampToString(f.UpdatedAt),
	}
}

type CreateSkillRequest struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Content     string                   `json:"content"`
	Config      any                      `json:"config"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

type CreateSkillFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type UpdateSkillRequest struct {
	Name        *string                  `json:"name"`
	Description *string                  `json:"description"`
	Content     *string                  `json:"content"`
	Config      any                      `json:"config"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

type SetAgentSkillsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

type AddAgentSkillsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

func validateFilePath(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return false
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") {
		return false
	}
	return true
}

func (h *Handler) loadSkillForUser(w http.ResponseWriter, r *http.Request, id string) (db.Skill, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Skill{}, false
	}

	skillUUID, ok := parseUUIDOrBadRequest(w, id, "skill id")
	if !ok {
		return db.Skill{}, false
	}

	skill, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
		ID:          skillUUID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return skill, false
	}
	return skill, true
}

func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	skills, err := h.Queries.ListSkillSummariesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SearchSkills(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	if !skillsources.EnabledFromEnv(skillsources.ClawHub) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"code":  "source_disabled",
			"error": skillsources.DisabledMessage(skillsources.ClawHub),
		})
		return
	}

	httpClient := newImportHTTPClient()
	candidates, err := searchClawHubSkills(httpClient, query)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"code":  "upstream_unavailable",
			"error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, candidates)
}

func (h *Handler) GetSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}

	fileResps := make([]SkillFileResponse, len(files))
	for i, f := range files {
		fileResps[i] = skillFileToResponse(f)
	}

	writeJSON(w, http.StatusOK, SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	})
}

func (h *Handler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	var req CreateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	for _, f := range req.Files {
		if !validateFilePath(f.Path) {
			writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
			return
		}
	}

	resp, err := h.createSkillWithFiles(r.Context(), skillCreateInput{
		WorkspaceID: workspaceUUID,
		CreatorID:   creatorUUID,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Config:      req.Config,
		Files:       req.Files,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) canManageSkill(w http.ResponseWriter, r *http.Request, skill db.Skill) bool {
	wsID := uuidToString(skill.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "skill not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isSkillCreator := skill.CreatedBy.Valid && uuidToString(skill.CreatedBy) == requestUserID(r)
	if !isAdmin && !isSkillCreator {
		writeError(w, http.StatusForbidden, "only the skill creator can manage this skill")
		return false
	}
	return true
}

func canOverwriteSkillByLocalImport(userID string, skill db.Skill) bool {
	return skill.CreatedBy.Valid && uuidToString(skill.CreatedBy) == userID
}

func (h *Handler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	var req UpdateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, f := range req.Files {
		if !validateFilePath(f.Path) {
			writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	params := db.UpdateSkillParams{
		ID: parseUUID(id),
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: sanitizeNullBytes(*req.Name), Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(*req.Description), Valid: true}
	}
	if req.Content != nil {
		params.Content = pgtype.Text{String: sanitizeNullBytes(*req.Content), Valid: true}
	}
	if req.Config != nil {
		config, _ := json.Marshal(req.Config)
		params.Config = config
	}

	skill, err = qtx.UpdateSkill(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update skill: "+err.Error())
		return
	}

	var fileResps []SkillFileResponse
	if req.Files != nil {
		if err := qtx.DeleteSkillFilesBySkill(r.Context(), skill.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete old skill files")
			return
		}
		fileResps = make([]SkillFileResponse, 0, len(req.Files))
		for _, f := range req.Files {

			if skillpkg.IsReservedContentPath(f.Path) {
				continue
			}
			sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
				SkillID: skill.ID,
				Path:    sanitizeNullBytes(f.Path),
				Content: sanitizeNullBytes(f.Content),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to upsert skill file: "+err.Error())
				return
			}
			fileResps = append(fileResps, skillFileToResponse(sf))
		}
	} else {
		files, _ := qtx.ListSkillFiles(r.Context(), skill.ID)
		fileResps = make([]SkillFileResponse, len(files))
		for i, f := range files {
			fileResps[i] = skillFileToResponse(f)
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	resp := SkillWithFilesResponse{
		SkillResponse: skillToResponse(skill),
		Files:         fileResps,
	}
	wsID := h.resolveWorkspaceID(r)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	h.publish(protocol.EventSkillUpdated, wsID, actorType, actorID, map[string]any{"skill": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteSkillLabelAssignmentsBySkill(r.Context(), skill.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove skill label assignments")
		return
	}
	if err := qtx.DeleteSkill(r.Context(), db.DeleteSkillParams{
		ID:          skill.ID,
		WorkspaceID: skill.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit skill deletion")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(skill.WorkspaceID))
	h.publish(protocol.EventSkillDeleted, uuidToString(skill.WorkspaceID), actorType, actorID, map[string]any{"skill_id": uuidToString(skill.ID)})
	w.WriteHeader(http.StatusNoContent)
}

type ImportSkillRequest struct {
	URL        string `json:"url"`
	OnConflict string `json:"on_conflict,omitempty"`
}

const (
	importOnConflictFail      = "fail"
	importOnConflictOverwrite = "overwrite"
	importOnConflictRename    = "rename"
	importOnConflictSkip      = "skip"
)

const maxImportRenameAttempts = 50

func validImportOnConflict(strategy string) bool {
	switch strategy {
	case "", importOnConflictFail, importOnConflictOverwrite, importOnConflictRename, importOnConflictSkip:
		return true
	}
	return false
}

const (
	maxImportFileSize  = 1 << 20
	maxImportTotalSize = 8 << 20
	maxImportFileCount = 256
)

type importedSkill struct {
	name        string
	description string
	content     string
	files       []importedFile
	bundleSize  int
	origin      map[string]any
}

type importedFile struct {
	path    string
	content string
}

var errImportCapExceeded = errors.New("import cap exceeded")

var errImportSourceUnavailable = errors.New("import source temporarily unavailable")

func isCapError(err error) bool {
	return errors.Is(err, errImportCapExceeded)
}

func (s *importedSkill) addFile(path, content string) error {
	if isLikelyBinaryFilePath(path) {
		slog.Info("skill import: skipping binary file", "path", path, "size", len(content))
		return nil
	}
	if len(s.files) >= maxImportFileCount {
		return fmt.Errorf("%w: import bundle exceeds %d file limit", errImportCapExceeded, maxImportFileCount)
	}
	if s.bundleSize+len(content) > maxImportTotalSize {
		return fmt.Errorf("%w: import bundle exceeds %d byte limit", errImportCapExceeded, maxImportTotalSize)
	}
	s.bundleSize += len(content)
	s.files = append(s.files, importedFile{path: path, content: content})
	return nil
}

func isLikelyBinaryFilePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case

		".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".ico", ".heic",

		".ttf", ".otf", ".woff", ".woff2", ".eot",

		".zip", ".gz", ".tar", ".bz2", ".7z", ".rar",

		".pdf", ".docx", ".xlsx", ".pptx", ".doc", ".xls", ".ppt",

		".mp3", ".mp4", ".wav", ".avi", ".mov", ".webm", ".m4a", ".flac",

		".exe", ".dll", ".so", ".dylib", ".class", ".jar", ".wasm",

		".db", ".sqlite", ".sqlite3", ".pyc":
		return true
	}
	return false
}

var clawHubAPIBase = "https://clawhub.ai/api/v1"

const clawHubSearchStatsLimit = 10

type clawhubSearchResponse struct {
	Results []clawhubSearchResult `json:"results"`
}

type clawhubSearchResult struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Summary     string `json:"summary"`
	OwnerHandle string `json:"ownerHandle"`
}

type clawhubSkillStats struct {
	InstallsAllTime int64 `json:"installsAllTime"`
	InstallsCurrent int64 `json:"installsCurrent"`
}

type clawhubGetSkillResponse struct {
	Skill         clawhubSkill          `json:"skill"`
	LatestVersion *clawhubLatestVersion `json:"latestVersion"`
}

type clawhubSkill struct {
	Slug        string            `json:"slug"`
	DisplayName string            `json:"displayName"`
	Summary     string            `json:"summary"`
	Tags        map[string]string `json:"tags"`
	Stats       clawhubSkillStats `json:"stats"`
}

type clawhubLatestVersion struct {
	Version string `json:"version"`
}

type clawhubVersionDetailResponse struct {
	Version clawhubVersionDetail `json:"version"`
}

type clawhubVersionDetail struct {
	Version string             `json:"version"`
	Files   []clawhubFileEntry `json:"files"`
}

type clawhubFileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
}

type githubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type githubTreeResponse struct {
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

func fetchGitHubDefaultBranch(ctx context.Context, httpClient *http.Client, owner, repo string) string {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo))
	resp, err := doGitHubAPIGet(ctx, httpClient, apiURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return "main"
	}
	defer resp.Body.Close()

	var info githubRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.DefaultBranch == "" {
		return "main"
	}
	return info.DefaultBranch
}

type importSource int

const (
	sourceClawHub importSource = iota
	sourceSkillsSh
	sourceGitHub
)

var newImportHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func skillSourceFor(source importSource) skillsources.Source {
	switch source {
	case sourceSkillsSh:
		return skillsources.SkillsSh
	case sourceGitHub:
		return skillsources.GitHub
	default:
		return skillsources.ClawHub
	}
}

func requireImportSourceEnabled(w http.ResponseWriter, source importSource) bool {
	src := skillSourceFor(source)
	if skillsources.EnabledFromEnv(src) {
		return true
	}
	writeError(w, http.StatusForbidden, skillsources.DisabledMessage(src))
	return false
}

func detectImportSource(raw string) (importSource, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", fmt.Errorf("empty URL")
	}

	normalized := raw
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return 0, "", fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "skills.sh" || host == "www.skills.sh":
		return sourceSkillsSh, normalized, nil
	case host == "clawhub.ai" || host == "www.clawhub.ai":
		return sourceClawHub, normalized, nil
	case host == "github.com" || host == "www.github.com":
		return sourceGitHub, normalized, nil
	default:

		if !strings.Contains(raw, "/") || !strings.Contains(raw, ".") {
			return sourceClawHub, raw, nil
		}
		return 0, "", fmt.Errorf("unsupported source: %s (supported: clawhub.ai, skills.sh, github.com)", host)
	}
}

func parseClawHubSlug(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")

	if len(parts) == 2 {
		return parts[1], nil
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}

	if raw == parsed.Host || parsed.Path == "" || parsed.Path == "/" {
		return "", fmt.Errorf("missing skill slug in URL")
	}
	return "", fmt.Errorf("could not extract skill slug from URL: %s", raw)
}

func searchClawHubSkills(httpClient *http.Client, query string) ([]SkillSearchCandidateResponse, error) {
	searchURL := clawHubAPIBase + "/search?q=" + url.QueryEscape(query)
	resp, err := httpClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to reach ClawHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClawHub search returned status %d", resp.StatusCode)
	}

	var searchResp clawhubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse ClawHub search response")
	}

	candidates := make([]SkillSearchCandidateResponse, 0, len(searchResp.Results))
	for i, result := range searchResp.Results {
		if result.Slug == "" {
			continue
		}
		candidate := SkillSearchCandidateResponse{
			Name:        result.DisplayName,
			URL:         buildClawHubSkillURL(result.OwnerHandle, result.Slug),
			Source:      "clawhub.ai",
			Description: result.Summary,
		}
		if candidate.Name == "" {
			candidate.Name = result.Slug
		}
		if i < clawHubSearchStatsLimit {
			if count, ok := fetchClawHubInstallCount(httpClient, result.Slug); ok {
				candidate.InstallCount = &count
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func buildClawHubSkillURL(ownerHandle, slug string) string {
	if ownerHandle == "" {
		return "https://clawhub.ai/" + url.PathEscape(slug)
	}
	return "https://clawhub.ai/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug)
}

func fetchClawHubInstallCount(httpClient *http.Client, slug string) (int64, bool) {
	detailURL := clawHubAPIBase + "/skills/" + url.PathEscape(slug)
	resp, err := httpClient.Get(detailURL)
	if err != nil {
		slog.Warn("clawhub search: failed to fetch skill details", "slug", slug, "error", err)
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("clawhub search: skill details returned non-200", "slug", slug, "status", resp.StatusCode)
		return 0, false
	}
	var detail clawhubGetSkillResponse
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		slog.Warn("clawhub search: failed to parse skill details", "slug", slug, "error", err)
		return 0, false
	}
	if detail.Skill.Stats.InstallsAllTime > 0 {
		return detail.Skill.Stats.InstallsAllTime, true
	}
	return detail.Skill.Stats.InstallsCurrent, true
}

func fetchFromClawHub(ctx context.Context, httpClient *http.Client, rawURL string) (*importedSkill, error) {
	slug, err := parseClawHubSlug(rawURL)
	if err != nil {
		return nil, err
	}

	apiBase := clawHubAPIBase

	skillReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/skills/"+url.PathEscape(slug), nil)
	if err != nil {
		return nil, err
	}
	skillResp, err := httpClient.Do(skillReq)
	if err != nil {
		return nil, fmt.Errorf("failed to reach ClawHub: %w", err)
	}
	defer skillResp.Body.Close()

	if skillResp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("skill not found on ClawHub: %s", slug)
	}
	if skillResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClawHub returned status %d", skillResp.StatusCode)
	}

	var chResp clawhubGetSkillResponse
	if err := json.NewDecoder(skillResp.Body).Decode(&chResp); err != nil {
		return nil, fmt.Errorf("failed to parse ClawHub response")
	}
	chSkill := chResp.Skill

	latestVersion := ""
	if v, ok := chSkill.Tags["latest"]; ok {
		latestVersion = v
	} else if chResp.LatestVersion != nil {
		latestVersion = chResp.LatestVersion.Version
	}

	var filePaths []string
	if latestVersion != "" {
		vURL := fmt.Sprintf("%s/skills/%s/versions/%s", apiBase, url.PathEscape(slug), url.PathEscape(latestVersion))
		vReq, verr := http.NewRequestWithContext(ctx, http.MethodGet, vURL, nil)
		if verr != nil {
			return nil, verr
		}
		vResp, err := httpClient.Do(vReq)
		if err == nil {
			defer vResp.Body.Close()
			if vResp.StatusCode == http.StatusOK {
				var vDetail clawhubVersionDetailResponse
				if err := json.NewDecoder(vResp.Body).Decode(&vDetail); err == nil {
					for _, f := range vDetail.Version.Files {
						filePaths = append(filePaths, f.Path)
					}
				}
			}
		}
	}

	result := &importedSkill{
		name:        chSkill.DisplayName,
		description: chSkill.Summary,
		origin: map[string]any{
			"type":       "clawhub",
			"source_url": rawURL,
			"slug":       slug,
		},
	}
	if result.name == "" {
		result.name = slug
	}

	for _, fp := range filePaths {
		fileURL := fmt.Sprintf("%s/skills/%s/file?path=%s", apiBase, url.PathEscape(slug), url.QueryEscape(fp))
		if latestVersion != "" {
			fileURL += "&version=" + url.QueryEscape(latestVersion)
		}
		body, err := fetchRawFile(ctx, httpClient, fileURL)
		if err != nil {

			if isCapError(err) || fp == "SKILL.md" {
				return nil, fmt.Errorf("clawhub import: %s: %w", fp, err)
			}

			if ctx.Err() != nil {
				return nil, fmt.Errorf("clawhub import: fetch aborted at %s: %w", fp, ctx.Err())
			}
			slog.Warn("clawhub import: file download failed", "path", fp, "error", err)
			continue
		}
		if fp == "SKILL.md" {
			result.content = string(body)
			continue
		}
		if err := result.addFile(fp, string(body)); err != nil {
			return nil, err
		}
	}

	if result.content == "" {
		return nil, fmt.Errorf("clawhub import: SKILL.md is empty or missing for %s", slug)
	}

	return result, nil
}

func parseSkillsShParts(raw string) (owner, repo, skillName string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("expected URL format: skills.sh/{owner}/{repo}/{skill-name}, got: %s", parsed.Path)
	}
	return parts[0], parts[1], parts[2], nil
}

func fetchFromSkillsSh(ctx context.Context, httpClient *http.Client, rawURL string) (*importedSkill, error) {
	owner, repo, skillName, err := parseSkillsShParts(rawURL)
	if err != nil {
		return nil, err
	}

	defaultBranch := fetchGitHubDefaultBranch(ctx, httpClient, owner, repo)
	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(defaultBranch))

	tree, truncated, treeErr := fetchGitHubTree(ctx, httpClient, owner, repo, defaultBranch)
	if treeErr != nil {

		slog.Warn("skills.sh import: repository tree fetch failed",
			"owner", owner, "repo", repo, "error", treeErr)
		return nil, fmt.Errorf("%w: could not read the %s/%s repository tree (usually GitHub API rate limiting — set GITHUB_TOKEN on the server or retry): %v",
			errImportSourceUnavailable, owner, repo, treeErr)
	}

	skillDir, skillMdBody, err := resolveSkillDirFromTree(ctx, httpClient, owner, repo, defaultBranch, rawPrefix, skillName, tree, truncated)
	if err != nil {
		return nil, err
	}

	result := newSkillsShImportedSkill(skillMdBody, skillName, rawURL, owner, repo)

	if truncated {

		if err := addSupportingFilesViaCrawl(ctx, httpClient, result, owner, repo, defaultBranch, skillDir); err != nil {
			return nil, err
		}
		return result, nil
	}

	if err := addSupportingFilesFromTree(ctx, httpClient, result, tree, rawPrefix, skillDir); err != nil {
		return nil, err
	}
	return result, nil
}

func newSkillsShImportedSkill(skillMdBody []byte, skillName, rawURL, owner, repo string) *importedSkill {
	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		name = skillName
	}
	return &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "skills_sh",
			"source_url": rawURL,
			"owner":      owner,
			"repo":       repo,
			"skill":      skillName,
		},
	}
}

func addSupportingFilesViaCrawl(ctx context.Context, httpClient *http.Client, result *importedSkill, owner, repo, ref, skillDir string) error {
	apiURL := buildGitHubContentsURL(owner, repo, skillDir, ref)
	dirResp, err := doGitHubAPIGet(ctx, httpClient, apiURL)
	if err != nil || dirResp.StatusCode != http.StatusOK {
		if dirResp != nil {
			dirResp.Body.Close()
		}

		if ctx.Err() != nil {
			return fmt.Errorf("github import: directory listing aborted: %w", ctx.Err())
		}
		return nil
	}
	defer dirResp.Body.Close()

	var entries []githubContentEntry
	if err := json.NewDecoder(dirResp.Body).Decode(&entries); err != nil {

		if ctx.Err() != nil {
			return fmt.Errorf("github import: directory listing read aborted: %w", ctx.Err())
		}
		slog.Warn("github import: failed to decode top-level directory listing", "url", apiURL, "error", err)
		return nil
	}

	var allFiles []githubContentEntry
	collectGitHubFiles(ctx, httpClient, entries, &allFiles, apiURL)

	if ctx.Err() != nil {
		return fmt.Errorf("github import: directory crawl aborted: %w", ctx.Err())
	}

	basePath := ""
	if skillDir != "" {
		basePath = skillDir + "/"
	}
	for _, entry := range allFiles {
		if entry.DownloadURL == "" {
			continue
		}
		body, err := fetchRawFile(ctx, httpClient, entry.DownloadURL)
		if err != nil {
			if isCapError(err) {
				return fmt.Errorf("github import: %s: %w", entry.Path, err)
			}
			if ctx.Err() != nil {
				return fmt.Errorf("github import: fetch aborted at %s: %w", entry.Path, ctx.Err())
			}
			slog.Warn("github import: file download failed", "path", entry.Path, "error", err)
			continue
		}
		relPath := strings.TrimPrefix(entry.Path, basePath)
		if err := result.addFile(relPath, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func fetchGitHubTree(ctx context.Context, httpClient *http.Client, owner, repo, ref string) (entries []githubTreeEntry, truncated bool, err error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	resp, err := doGitHubAPIGet(ctx, httpClient, apiURL)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var tree githubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, false, err
	}
	return tree.Tree, tree.Truncated, nil
}

func resolveSkillDirFromTree(ctx context.Context, httpClient *http.Client, owner, repo, defaultBranch, rawPrefix, skillName string, tree []githubTreeEntry, truncated bool) (string, []byte, error) {
	skillPaths := extractSkillMdPaths(tree)
	preferred, remaining := partitionSkillMdPaths(skillName, skillPaths)
	if dir, body, ok := findMatchingSkillDirByFrontmatter(ctx, httpClient, rawPrefix, skillName, preferred); ok {
		return dir, body, nil
	}
	if !truncated {
		if dir, body, ok := findMatchingSkillDirByFrontmatter(ctx, httpClient, rawPrefix, skillName, remaining); ok {
			return dir, body, nil
		}

		if dir, body, ok := acceptConventionalSkillDir(ctx, httpClient, rawPrefix, skillName, skillPaths, true); ok {
			return dir, body, nil
		}
		return "", nil, skillMdNotFoundError(owner, repo, skillName)
	}

	slog.Warn("github import: repository tree listing truncated", "owner", owner, "repo", repo, "branch", defaultBranch)
	if dir, body, ok := findSkillDirFromConventionalPrefixes(ctx, httpClient, owner, repo, defaultBranch, rawPrefix, skillName); ok {
		return dir, body, nil
	}

	if dir, body, ok := acceptConventionalSkillDir(ctx, httpClient, rawPrefix, skillName, skillPaths, false); ok {
		return dir, body, nil
	}
	return "", nil, fmt.Errorf("repository %s/%s tree is too large to scan exhaustively for skill %s", owner, repo, skillName)
}

func conventionalSkillMdPaths(skillName string) []string {
	return []string{
		"skills/" + skillName + "/SKILL.md",
		".claude/skills/" + skillName + "/SKILL.md",
		"plugin/skills/" + skillName + "/SKILL.md",
		skillName + "/SKILL.md",
	}
}

func acceptConventionalSkillDir(ctx context.Context, httpClient *http.Client, rawPrefix, skillName string, treeSkillPaths []string, treeComplete bool) (string, []byte, bool) {
	present := make(map[string]struct{}, len(treeSkillPaths))
	for _, p := range treeSkillPaths {
		present[p] = struct{}{}
	}
	for _, candidate := range conventionalSkillMdPaths(skillName) {
		if treeComplete {
			if _, ok := present[candidate]; !ok {
				continue
			}
		}
		body, err := fetchRawFile(ctx, httpClient, buildRawGitHubURL(rawPrefix, candidate))
		if err != nil {

			if treeComplete {
				slog.Warn("skills.sh import: conventional SKILL.md fetch failed", "path", candidate, "error", err)
			}
			continue
		}
		return skillDirFromSkillFilePath(candidate), body, true
	}
	return "", nil, false
}

const treeDownloadConcurrency = 8

func addSupportingFilesFromTree(ctx context.Context, httpClient *http.Client, result *importedSkill, tree []githubTreeEntry, rawPrefix, skillDir string) error {
	basePath := ""
	if skillDir != "" {
		basePath = skillDir + "/"
	}

	type treeFile struct {
		repoPath string
		relPath  string
		size     int64
	}
	var eligible []treeFile
	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}
		if basePath != "" && !strings.HasPrefix(entry.Path, basePath) {
			continue
		}
		relPath := strings.TrimPrefix(entry.Path, basePath)
		if relPath == "" {
			continue
		}
		lowerBase := strings.ToLower(filepath.Base(relPath))
		if lowerBase == "skill.md" || lowerBase == "license" || lowerBase == "license.txt" || lowerBase == "license.md" {
			continue
		}
		if isLikelyBinaryFilePath(relPath) {
			continue
		}
		eligible = append(eligible, treeFile{repoPath: entry.Path, relPath: relPath, size: entry.size()})
	}

	sort.Slice(eligible, func(i, j int) bool { return eligible[i].relPath < eligible[j].relPath })

	if len(eligible) > maxImportFileCount {
		return fmt.Errorf("%w: import bundle would contain %d files, exceeding the %d file limit", errImportCapExceeded, len(eligible), maxImportFileCount)
	}
	var totalSize int64
	for _, f := range eligible {
		if f.size > maxImportFileSize {
			return fmt.Errorf("%w: %s is %d bytes, exceeding the %d byte per-file limit", errImportCapExceeded, f.relPath, f.size, maxImportFileSize)
		}
		totalSize += f.size
	}
	if totalSize > maxImportTotalSize {
		return fmt.Errorf("%w: import bundle is %d bytes, exceeding the %d byte limit", errImportCapExceeded, totalSize, maxImportTotalSize)
	}

	contents := make([]string, len(eligible))
	fetched := make([]bool, len(eligible))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(treeDownloadConcurrency)
	for i, f := range eligible {
		i, f := i, f
		g.Go(func() error {
			body, err := fetchRawFile(gctx, httpClient, buildRawGitHubURL(rawPrefix, f.repoPath))
			if err != nil {
				if isCapError(err) {
					return fmt.Errorf("github import: %s: %w", f.repoPath, err)
				}

				if gctx.Err() != nil {
					return fmt.Errorf("github import: fetch aborted at %s: %w", f.repoPath, gctx.Err())
				}

				slog.Warn("github import: file download failed", "path", f.repoPath, "error", err)
				return nil
			}
			contents[i] = string(body)
			fetched[i] = true
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for i, f := range eligible {
		if !fetched[i] {
			continue
		}
		if err := result.addFile(f.relPath, contents[i]); err != nil {
			return err
		}
	}
	return nil
}

func (e githubTreeEntry) size() int64 {
	if e.Size < 0 {
		return 0
	}
	return e.Size
}

func collectGitHubFiles(ctx context.Context, httpClient *http.Client, entries []githubContentEntry, out *[]githubContentEntry, parentURL string) {
	for _, entry := range entries {

		if ctx.Err() != nil {
			return
		}
		lower := strings.ToLower(entry.Name)
		if lower == "skill.md" || lower == "license" || lower == "license.txt" || lower == "license.md" {
			continue
		}
		if entry.Type == "file" {
			*out = append(*out, entry)
		} else if entry.Type == "dir" {

			subURL := entry.URL
			if subURL == "" {
				parsed, err := url.Parse(parentURL)
				if err != nil {
					slog.Warn("github import: invalid parent directory url", "url", parentURL, "error", err)
					continue
				}
				parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + entry.Name
				subURL = parsed.String()
			}
			subResp, err := doGitHubAPIGet(ctx, httpClient, subURL)
			if err != nil || subResp.StatusCode != http.StatusOK {
				attrs := []any{"url", subURL}
				if subResp != nil {
					attrs = append(attrs, "status", subResp.StatusCode)
					subResp.Body.Close()
				}
				if err != nil {
					attrs = append(attrs, "error", err)
				}
				slog.Warn("github import: failed to list subdirectory", attrs...)
				continue
			}
			var subEntries []githubContentEntry
			if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
				subResp.Body.Close()
				slog.Warn("github import: failed to decode subdirectory listing", "url", subURL, "error", err)
				continue
			}
			subResp.Body.Close()
			collectGitHubFiles(ctx, httpClient, subEntries, out, subURL)
		}
	}
}

func findSkillDirFromConventionalPrefixes(ctx context.Context, httpClient *http.Client, owner, repo, defaultBranch, rawPrefix, skillName string) (string, []byte, bool) {
	prefixes := []string{"skills", ".claude/skills", "plugin/skills"}
	var skillPaths []string
	for _, prefix := range prefixes {
		paths, err := listGitHubSkillMdPaths(ctx, httpClient, owner, repo, prefix, defaultBranch)
		if err != nil {
			slog.Warn("github import: failed to list conventional skill prefix", "prefix", prefix, "error", err)
			continue
		}
		skillPaths = append(skillPaths, paths...)
	}

	preferred, remaining := partitionSkillMdPaths(skillName, skillPaths)
	if dir, body, ok := findMatchingSkillDirByFrontmatter(ctx, httpClient, rawPrefix, skillName, preferred); ok {
		return dir, body, true
	}
	return findMatchingSkillDirByFrontmatter(ctx, httpClient, rawPrefix, skillName, remaining)
}

func listGitHubSkillMdPaths(ctx context.Context, httpClient *http.Client, owner, repo, repoPath, ref string) ([]string, error) {
	apiURL := buildGitHubContentsURL(owner, repo, repoPath, ref)
	resp, err := doGitHubAPIGet(ctx, httpClient, apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var entries []githubContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}

	var paths []string
	collectGitHubSkillMdPaths(ctx, httpClient, entries, &paths, apiURL)
	return paths, nil
}

func collectGitHubSkillMdPaths(ctx context.Context, httpClient *http.Client, entries []githubContentEntry, out *[]string, parentURL string) {
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		if entry.Type == "file" {
			if lower == "skill.md" {
				*out = append(*out, entry.Path)
			}
			continue
		}
		if entry.Type != "dir" {
			continue
		}

		subURL := entry.URL
		if subURL == "" {
			parsed, err := url.Parse(parentURL)
			if err != nil {
				slog.Warn("github import: invalid parent directory url", "url", parentURL, "error", err)
				continue
			}
			parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + entry.Name
			subURL = parsed.String()
		}

		subResp, err := doGitHubAPIGet(ctx, httpClient, subURL)
		if err != nil || subResp.StatusCode != http.StatusOK {
			attrs := []any{"url", subURL}
			if subResp != nil {
				attrs = append(attrs, "status", subResp.StatusCode)
				subResp.Body.Close()
			}
			if err != nil {
				attrs = append(attrs, "error", err)
			}
			slog.Warn("github import: failed to list skill metadata subdirectory", attrs...)
			continue
		}

		var subEntries []githubContentEntry
		if err := json.NewDecoder(subResp.Body).Decode(&subEntries); err != nil {
			subResp.Body.Close()
			slog.Warn("github import: failed to decode skill metadata subdirectory", "url", subURL, "error", err)
			continue
		}
		subResp.Body.Close()
		collectGitHubSkillMdPaths(ctx, httpClient, subEntries, out, subURL)
	}
}

func extractSkillMdPaths(entries []githubTreeEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "blob" || (!strings.HasSuffix(entry.Path, "/SKILL.md") && entry.Path != "SKILL.md") {
			continue
		}
		paths = append(paths, entry.Path)
	}
	return paths
}

func partitionSkillMdPaths(skillName string, skillPaths []string) (preferred []string, remaining []string) {
	for _, skillPath := range skillPaths {
		if isLikelySkillPathMatch(skillName, skillPath) {
			preferred = append(preferred, skillPath)
			continue
		}
		remaining = append(remaining, skillPath)
	}
	return preferred, remaining
}

func findMatchingSkillDirByFrontmatter(ctx context.Context, httpClient *http.Client, rawPrefix, skillName string, skillPaths []string) (string, []byte, bool) {
	for _, skillPath := range skillPaths {
		body, err := fetchRawFile(ctx, httpClient, buildRawGitHubURL(rawPrefix, skillPath))
		if err != nil {
			slog.Warn("github import: fallback SKILL.md fetch failed", "path", skillPath, "error", err)
			continue
		}
		name, _ := skillpkg.ParseSkillFrontmatter(string(body))
		if name == skillName {
			return skillDirFromSkillFilePath(skillPath), body, true
		}
	}
	return "", nil, false
}

func isLikelySkillPathMatch(skillName, skillPath string) bool {
	dir := strings.ToLower(skillDirFromSkillFilePath(skillPath))
	base := strings.ToLower(filepath.Base(dir))
	for _, hint := range skillNameHints(skillName) {
		if strings.Contains(dir, hint) || strings.Contains(base, hint) || strings.Contains(hint, base) {
			return true
		}
	}
	return false
}

func skillNameHints(skillName string) []string {
	skillName = strings.ToLower(skillName)
	parts := strings.Split(skillName, "-")
	seen := map[string]struct{}{}
	var hints []string

	addHint := func(value string) {
		value = strings.TrimSpace(value)
		if len(value) < 3 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		hints = append(hints, value)
	}

	addHint(skillName)
	for i := 1; i < len(parts); i++ {
		addHint(strings.Join(parts[i:], "-"))
	}
	for _, part := range parts {
		addHint(part)
	}
	return hints
}

var errGitHubAPIBlocked = errors.New("github API blocked (rate limit or auth)")

func doGitHubAPIGet(ctx context.Context, httpClient *http.Client, apiURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	addGitHubAuthHeader(req)
	return httpClient.Do(req)
}

func addGitHubAuthHeader(req *http.Request) {
	if req == nil {
		return
	}

	if !skillsources.EnabledFromEnv(skillsources.GitHub) {
		return
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

type githubSpec struct {
	owner    string
	repo     string
	ref      string
	skillDir string

	refSegments []string

	kind string
}

func parseGitHubURL(raw string) (githubSpec, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return githubSpec{}, fmt.Errorf("invalid URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return githubSpec{}, fmt.Errorf("expected URL format: github.com/{owner}/{repo}[/tree/{ref}/{path}], got: %s", parsed.Path)
	}
	spec := githubSpec{owner: parts[0], repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) == 2 {
		return spec, nil
	}
	kind := parts[2]
	if kind != "tree" && kind != "blob" {
		return githubSpec{}, fmt.Errorf("unsupported URL form: github.com/%s/%s/%s/... (use /tree/{ref}/... or /blob/{ref}/.../SKILL.md)", spec.owner, spec.repo, kind)
	}
	if len(parts) < 4 || parts[3] == "" {
		return githubSpec{}, fmt.Errorf("missing ref after /%s/", kind)
	}
	spec.kind = kind
	rest := parts[3:]
	if kind == "blob" {
		if !strings.EqualFold(rest[len(rest)-1], "SKILL.md") {
			return githubSpec{}, fmt.Errorf("blob URL must point to a SKILL.md file")
		}
		rest = rest[:len(rest)-1]
		if len(rest) == 0 {
			return githubSpec{}, fmt.Errorf("missing ref after /blob/")
		}
	}

	decoded := make([]string, len(rest))
	for i, p := range rest {
		d, err := url.PathUnescape(p)
		if err != nil {
			return githubSpec{}, fmt.Errorf("invalid path segment %q: %w", p, err)
		}
		if d == "" {
			return githubSpec{}, fmt.Errorf("empty path segment in URL")
		}
		decoded[i] = d
	}
	spec.refSegments = decoded

	spec.ref = decoded[0]
	if len(decoded) > 1 {
		spec.skillDir = strings.Join(decoded[1:], "/")
	}
	return spec, nil
}

func resolveGitHubRefAndPath(ctx context.Context, httpClient *http.Client, spec *githubSpec) error {
	if len(spec.refSegments) == 0 {
		return nil
	}

	tried := make([]string, 0, len(spec.refSegments))
	blocked := false
	for n := len(spec.refSegments); n >= 1; n-- {
		candidate := strings.Join(spec.refSegments[:n], "/")
		tried = append(tried, candidate)
		ok, err := githubRefExists(ctx, httpClient, spec.owner, spec.repo, candidate)
		if errors.Is(err, errGitHubAPIBlocked) {

			blocked = true
			continue
		}
		if err != nil {

			return fmt.Errorf("validating ref %q: %w", candidate, err)
		}
		if ok {
			spec.ref = candidate
			if n == len(spec.refSegments) {
				spec.skillDir = ""
			} else {
				spec.skillDir = strings.Join(spec.refSegments[n:], "/")
			}
			return nil
		}
	}
	if blocked {

		slog.Warn("github import: ref resolution blocked by GitHub API (rate limit or auth); falling back to optimistic single-segment ref. Set GITHUB_TOKEN to enable disambiguation of slash-bearing refs.",
			"owner", spec.owner, "repo", spec.repo, "tried", tried)
		return nil
	}
	return fmt.Errorf("could not resolve ref in github.com/%s/%s URL — tried: %s. Make sure the branch, tag, or commit exists and that the URL is the canonical /tree/{ref}/{path} or /blob/{ref}/{path}/SKILL.md form",
		spec.owner, spec.repo, strings.Join(tried, ", "))
}

func githubRefExists(ctx context.Context, httpClient *http.Client, owner, repo, ref string) (bool, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s",
		url.PathEscape(owner), url.PathEscape(repo), escapeRefPath(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3.sha")
	addGitHubAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return false, nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return false, errGitHubAPIBlocked
	default:
		return false, fmt.Errorf("github API returned status %d for ref %q", resp.StatusCode, ref)
	}
}

func fetchFromGitHub(ctx context.Context, httpClient *http.Client, rawURL string) (*importedSkill, error) {
	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {

		if err := resolveGitHubRefAndPath(ctx, httpClient, &spec); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		spec.ref = fetchGitHubDefaultBranch(ctx, httpClient, spec.owner, spec.repo)
	}
	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		url.PathEscape(spec.owner), url.PathEscape(spec.repo), escapeRefPath(spec.ref))

	skillMdPath := "SKILL.md"
	if spec.skillDir != "" {
		skillMdPath = spec.skillDir + "/SKILL.md"
	}
	skillMdBody, err := fetchRawFile(ctx, httpClient, buildRawGitHubURL(rawPrefix, skillMdPath))
	if err != nil {
		if spec.skillDir == "" {
			return nil, fmt.Errorf("SKILL.md not found at the root of %s/%s@%s. For multi-skill repositories, point to a specific directory using github.com/%s/%s/tree/%s/<skill-dir>",
				spec.owner, spec.repo, spec.ref, spec.owner, spec.repo, spec.ref)
		}
		return nil, fmt.Errorf("SKILL.md not found at %s in %s/%s@%s: %w",
			skillMdPath, spec.owner, spec.repo, spec.ref, err)
	}

	name, description := skillpkg.ParseSkillFrontmatter(string(skillMdBody))
	if name == "" {
		if spec.skillDir != "" {
			name = filepath.Base(spec.skillDir)
		} else {
			name = spec.repo
		}
	}

	result := &importedSkill{
		name:        name,
		description: description,
		content:     string(skillMdBody),
		origin: map[string]any{
			"type":       "github",
			"source_url": rawURL,
			"owner":      spec.owner,
			"repo":       spec.repo,
			"ref":        spec.ref,
			"path":       spec.skillDir,
		},
	}

	tree, truncated, treeErr := fetchGitHubTree(ctx, httpClient, spec.owner, spec.repo, spec.ref)
	if treeErr == nil && !truncated {
		if err := addSupportingFilesFromTree(ctx, httpClient, result, tree, rawPrefix, spec.skillDir); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := addSupportingFilesViaCrawl(ctx, httpClient, result, spec.owner, spec.repo, spec.ref, spec.skillDir); err != nil {
		return nil, err
	}
	return result, nil
}

const rawGitHubContentHost = "raw.githubusercontent.com"

func fetchRawFile(ctx context.Context, httpClient *http.Client, fileURL string) ([]byte, error) {
	req, err := newRawFileRequest(ctx, fileURL)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImportFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxImportFileSize {
		return nil, fmt.Errorf("%w: file exceeds %d byte limit", errImportCapExceeded, maxImportFileSize)
	}
	return body, nil
}

func newRawFileRequest(ctx context.Context, fileURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(req.URL.Hostname(), rawGitHubContentHost) {
		addGitHubAuthHeader(req)
	}
	return req, nil
}

func escapeRefPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func buildRawGitHubURL(rawPrefix, repoPath string) string {
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	if len(escaped) == 0 {
		return rawPrefix
	}
	return rawPrefix + "/" + strings.Join(escaped, "/")
}

func buildGitHubContentsURL(owner, repo, repoPath, ref string) string {
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents",
		url.PathEscape(owner), url.PathEscape(repo))
	if repoPath == "" {
		return base + "?ref=" + url.QueryEscape(ref)
	}
	return base + "/" + strings.TrimPrefix(buildRawGitHubURL("", repoPath), "/") + "?ref=" + url.QueryEscape(ref)
}

func skillDirFromSkillFilePath(path string) string {
	if path == "SKILL.md" {
		return ""
	}
	return strings.TrimSuffix(path, "/SKILL.md")
}

func skillMdNotFoundError(owner, repo, skillName string) error {
	return fmt.Errorf("SKILL.md not found in repository %s/%s for skill %s", owner, repo, skillName)
}

func skillImportConflictReason() string {
	return "a skill with this name already exists; use --on-conflict overwrite to replace it or --on-conflict rename to import a copy"
}

func (h *Handler) createImportedSkillWithName(ctx context.Context, workspaceID, creatorID pgtype.UUID, name string, imported *importedSkill, config map[string]any, files []CreateSkillFileRequest) (SkillWithFilesResponse, error) {
	return h.createSkillWithFiles(ctx, skillCreateInput{
		WorkspaceID: workspaceID,
		CreatorID:   creatorID,
		Name:        name,
		Description: imported.description,
		Content:     imported.content,
		Config:      config,
		Files:       files,
	})
}

func (h *Handler) createRenamedImportedSkill(ctx context.Context, workspaceID, creatorID pgtype.UUID, baseName string, imported *importedSkill, config map[string]any, files []CreateSkillFileRequest) (SkillWithFilesResponse, error) {
	for suffix := 2; suffix < maxImportRenameAttempts+2; suffix++ {
		candidate := fmt.Sprintf("%s-%d", baseName, suffix)
		resp, err := h.createImportedSkillWithName(ctx, workspaceID, creatorID, candidate, imported, config, files)
		if err == nil {
			return resp, nil
		}
		if !isUniqueViolation(err) {
			return SkillWithFilesResponse{}, err
		}
	}
	return SkillWithFilesResponse{}, fmt.Errorf("failed to find an available renamed skill name after %d attempts", maxImportRenameAttempts)
}

func skillImportOverwriteFailure(err error) (int, string) {
	switch {
	case errors.Is(err, errSkillOverwriteNotFound):
		return http.StatusConflict, "target skill no longer exists"
	case errors.Is(err, errSkillOverwriteForbidden):
		return http.StatusForbidden, "only the skill creator can overwrite this skill"
	case errors.Is(err, errSkillOverwriteNameMismatch):
		return http.StatusConflict, "target skill name no longer matches the imported skill"
	default:
		return http.StatusInternalServerError, "failed to overwrite skill: " + err.Error()
	}
}

func (h *Handler) resolveImportSkillConflict(w http.ResponseWriter, r *http.Request, strategy string, workspaceID string, workspaceUUID, creatorUUID pgtype.UUID, creatorID string, name string, imported *importedSkill, config map[string]any, files []CreateSkillFileRequest, existing db.Skill) {
	existingInfo := existingSkillIdentity(existing, creatorID)
	switch strategy {
	case importOnConflictSkip:
		writeJSON(w, http.StatusOK, SkillImportResult{
			Status:        "skipped",
			Reason:        "a skill with this name already exists",
			ExistingSkill: &existingInfo,
		})
	case importOnConflictOverwrite:
		if !canOverwriteSkillByLocalImport(creatorID, existing) {
			writeJSON(w, http.StatusForbidden, SkillImportResult{
				Status:        "failed",
				Reason:        "only the skill creator can overwrite this skill",
				ExistingSkill: &existingInfo,
			})
			return
		}
		resp, err := h.overwriteSkillWithFiles(r.Context(), skillOverwriteInput{
			WorkspaceID:   workspaceUUID,
			TargetSkillID: existing.ID,
			UserID:        creatorID,
			ExpectedName:  name,
			Description:   imported.description,
			Content:       imported.content,
			Config:        config,
			Files:         files,
		})
		if err != nil {
			status, reason := skillImportOverwriteFailure(err)
			writeJSON(w, status, SkillImportResult{
				Status:        "failed",
				Reason:        reason,
				ExistingSkill: &existingInfo,
			})
			return
		}
		actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
		h.publish(protocol.EventSkillUpdated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
		writeJSON(w, http.StatusOK, SkillImportResult{Status: "updated", Skill: &resp})
	case importOnConflictRename:
		resp, err := h.createRenamedImportedSkill(r.Context(), workspaceUUID, creatorUUID, name, imported, config, files)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, SkillImportResult{
				Status:        "failed",
				Reason:        "failed to create renamed skill: " + err.Error(),
				ExistingSkill: &existingInfo,
			})
			return
		}
		actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
		h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
		writeJSON(w, http.StatusCreated, SkillImportResult{
			Status:        "created",
			Reason:        "renamed to avoid an existing skill",
			Skill:         &resp,
			ExistingSkill: &existingInfo,
		})
	default:
		writeJSON(w, http.StatusConflict, SkillImportResult{
			Status:        "conflict",
			Reason:        skillImportConflictReason(),
			ExistingSkill: &existingInfo,
		})
	}
}

func (h *Handler) ImportSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	if isMultipartForm(r) {
		h.importSkillFromArchive(w, r, workspaceID, workspaceUUID, creatorUUID, creatorID)
		return
	}

	var req ImportSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validImportOnConflict(req.OnConflict) {
		writeError(w, http.StatusBadRequest, "on_conflict must be one of: fail, overwrite, rename, skip")
		return
	}
	structuredResult := req.OnConflict != ""
	strategy := req.OnConflict
	if strategy == "" {
		strategy = importOnConflictFail
	}

	source, normalized, err := detectImportSource(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !requireImportSourceEnabled(w, source) {
		return
	}

	httpClient := newImportHTTPClient()

	ctx, cancel := context.WithTimeout(r.Context(), importFetchTimeout)
	defer cancel()

	var imported *importedSkill
	switch source {
	case sourceClawHub:
		imported, err = fetchFromClawHub(ctx, httpClient, normalized)
	case sourceSkillsSh:
		imported, err = fetchFromSkillsSh(ctx, httpClient, normalized)
	case sourceGitHub:
		imported, err = fetchFromGitHub(ctx, httpClient, normalized)
	}
	if err != nil {
		status, msg := importFetchErrorResponse(ctx, err)
		writeError(w, status, msg)
		return
	}

	h.finishSkillImport(w, r, workspaceID, workspaceUUID, creatorUUID, creatorID, strategy, structuredResult, imported)
}

const importFetchTimeout = 45 * time.Second

func importFetchErrorResponse(ctx context.Context, err error) (int, string) {
	if isCapError(err) {
		return http.StatusRequestEntityTooLarge, err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		return http.StatusGatewayTimeout, "skill import timed out fetching source files; the skill may be too large or the source too slow"
	}
	if errors.Is(err, errImportSourceUnavailable) {
		return http.StatusServiceUnavailable, err.Error()
	}
	return http.StatusBadGateway, err.Error()
}

func (h *Handler) finishSkillImport(w http.ResponseWriter, r *http.Request, workspaceID string, workspaceUUID, creatorUUID pgtype.UUID, creatorID, strategy string, structuredResult bool, imported *importedSkill) {
	files := make([]CreateSkillFileRequest, 0, len(imported.files))
	for _, f := range imported.files {
		if !validateFilePath(f.path) {
			continue
		}
		files = append(files, CreateSkillFileRequest{
			Path:    f.path,
			Content: f.content,
		})
	}

	config := map[string]any{}
	if imported.origin != nil {
		config["origin"] = imported.origin
	}
	name := sanitizeNullBytes(imported.name)

	if structuredResult {
		if existing, found, lerr := h.lookupSkillByName(r.Context(), workspaceUUID, name); lerr != nil {
			writeJSON(w, http.StatusInternalServerError, SkillImportResult{
				Status: "failed",
				Reason: "failed to check for existing skill: " + lerr.Error(),
			})
			return
		} else if found {
			h.resolveImportSkillConflict(w, r, strategy, workspaceID, workspaceUUID, creatorUUID, creatorID, name, imported, config, files, existing)
			return
		}
	}

	resp, err := h.createImportedSkillWithName(r.Context(), workspaceUUID, creatorUUID, name, imported, config, files)
	if err != nil {
		if isUniqueViolation(err) {
			if structuredResult {
				if existing, found, lerr := h.lookupSkillByName(r.Context(), workspaceUUID, name); lerr == nil && found {
					h.resolveImportSkillConflict(w, r, strategy, workspaceID, workspaceUUID, creatorUUID, creatorID, name, imported, config, files, existing)
					return
				}
			}
			if existing, found, findErr := h.existingSkillIdentityByName(r.Context(), workspaceUUID, name); findErr == nil && found {
				writeSkillImportDuplicateConflict(w, existing)
			} else {
				writeError(w, http.StatusConflict, "a skill with this name already exists")
			}
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
		return
	}
	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)
	h.publish(protocol.EventSkillCreated, workspaceID, actorType, actorID, map[string]any{"skill": resp})
	if structuredResult {
		writeJSON(w, http.StatusCreated, SkillImportResult{Status: "created", Skill: &resp})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListSkillFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	files, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}

	resp := make([]SkillFileResponse, len(files))
	for i, f := range files {
		resp[i] = skillFileToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpsertSkillFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	var req CreateSkillFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validateFilePath(req.Path) {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}
	if skillpkg.IsReservedContentPath(req.Path) {
		writeError(w, http.StatusBadRequest, "SKILL.md is reserved for the primary skill content")
		return
	}

	sf, err := h.Queries.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
		SkillID: skill.ID,
		Path:    sanitizeNullBytes(req.Path),
		Content: sanitizeNullBytes(req.Content),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to upsert skill file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, skillFileToResponse(sf))
}

func (h *Handler) DeleteSkillFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	fileID := chi.URLParam(r, "fileId")
	fileUUID, ok := parseUUIDOrBadRequest(w, fileID, "file id")
	if !ok {
		return
	}

	file, err := h.Queries.GetSkillFile(r.Context(), fileUUID)
	if err != nil || uuidToString(file.SkillID) != uuidToString(skill.ID) {
		writeError(w, http.StatusNotFound, "skill file not found")
		return
	}
	if err := h.Queries.DeleteSkillFile(r.Context(), file.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skill file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
		resp[i].Enabled = &s.Enabled
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req SetAgentSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	if !h.validateAgentSkillIDsInWorkspace(w, r, agent, skillUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	if err := qtx.RemoveAllAgentSkills(r.Context(), agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear agent skills")
		return
	}

	for _, skillID := range skillUUIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: skillID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent skill: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) AddAgentSkills(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req AddAgentSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	if !h.validateAgentSkillIDsInWorkspace(w, r, agent, skillUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	for _, skillID := range skillUUIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: agent.ID,
			SkillID: skillID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent skill: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) SetAgentSkillEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	skillID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "skillId"), "skill_id")
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	rows, err := h.Queries.SetAgentSkillEnabled(r.Context(), db.SetAgentSkillEnabledParams{
		AgentID: agent.ID,
		SkillID: skillID,
		Enabled: *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent skill")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "agent skill not found")
		return
	}

	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) RemoveAgentSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	skillID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "skillId"), "skill_id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveAgentSkill(r.Context(), db.RemoveAgentSkillParams{
		AgentID: agent.ID,
		SkillID: skillID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove agent skill")
		return
	}
	h.writeUpdatedAgentSkills(w, r, agent)
}

func (h *Handler) validateAgentSkillIDsInWorkspace(w http.ResponseWriter, r *http.Request, agent db.Agent, skillUUIDs []pgtype.UUID) bool {
	seen := map[string]struct{}{}
	for _, skillID := range skillUUIDs {
		key := uuidToString(skillID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skillID,
			WorkspaceID: agent.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return false
		}
	}
	return true
}

func (h *Handler) writeUpdatedAgentSkills(w http.ResponseWriter, r *http.Request, agent db.Agent) {
	skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}

	resp := make([]SkillSummaryResponse, len(skills))
	for i, s := range skills {
		resp[i] = skillSummaryToResponse(
			s.ID, s.WorkspaceID, s.Name, s.Description, s.Config,
			s.CreatedBy, s.CreatedAt, s.UpdatedAt,
		)
		resp[i].Enabled = &s.Enabled
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent_id": uuidToString(agent.ID), "skills": resp})
	writeJSON(w, http.StatusOK, resp)
}
