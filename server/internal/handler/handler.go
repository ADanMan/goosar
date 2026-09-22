package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/cloudruntime"
	"github.com/adanman/goosar/server/internal/corpauth"
	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/deploymentprofile"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	composio "github.com/adanman/goosar/server/internal/integrations/composio"
	"github.com/adanman/goosar/server/internal/integrations/ghsnapshot"
	"github.com/adanman/goosar/server/internal/integrations/slack"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/internal/provisioning"
	"github.com/adanman/goosar/server/internal/realtime"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/storage"
	"github.com/adanman/goosar/server/internal/util"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/featureflag"
	"github.com/adanman/goosar/server/pkg/llm"
)

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Config struct {
	AllowSignup         bool
	AllowedEmails       []string
	AllowedEmailDomains []string

	DisableWorkspaceCreation bool

	VCSIntegrationEnabled bool

	PublicURL string

	TrustedProxies []netip.Prefix

	CloudRuntimeFleetURL     string
	CloudRuntimeFleetTimeout time.Duration
	AttachmentDownloadMode   string
	AttachmentDownloadURLTTL time.Duration

	AttachmentFrameAncestors []string

	LLMAPIKey       string
	LLMBaseURL      string
	LLMDefaultModel string

	ServerVersion string

	DeliveryProfile deliveryprofile.Profile

	DeploymentProfile deploymentprofile.Profile

	MCPPolicy *perimeterpolicy.MCPPolicy

	AllowedProviders *perimeterpolicy.ProviderPolicy
}

type cloudRuntimeProxy interface {
	Enabled() bool
	Do(ctx context.Context, req cloudruntime.Request) (*cloudruntime.Response, error)
}

type RuntimeProfileRefreshNotifier interface {
	NotifyRuntimeProfilesChanged(workspaceID, profileID string)
}

type WorkspaceSetRefreshNotifier interface {
	NotifyWorkspacesChanged(userID string)
}

type Handler struct {
	Queries   *db.Queries
	DB        dbExecutor
	TxStarter txStarter
	Hub       *realtime.Hub

	Disconnector           realtime.UserDisconnector
	DaemonHub              *daemonws.Hub
	DaemonProfileRefresh   RuntimeProfileRefreshNotifier
	DaemonWorkspaceRefresh WorkspaceSetRefreshNotifier
	Bus                    *events.Bus
	TaskService            *service.TaskService
	IssueService           *service.IssueService
	AutopilotService       *service.AutopilotService

	EmailService          EmailSender
	UpdateStore           UpdateStore
	ModelListStore        ModelListStore
	LocalSkillListStore   LocalSkillListStore
	LocalSkillImportStore LocalSkillImportStore

	FeatureFlags       *featureflag.Service
	LivenessStore      LivenessStore
	HeartbeatScheduler HeartbeatScheduler
	Storage            storage.Storage
	CFSigner           *auth.CloudFrontSigner
	Analytics          analytics.Client

	Metrics          *obsmetrics.BusinessMetrics
	PATCache         *auth.PATCache
	DaemonTokenCache *auth.DaemonTokenCache
	MembershipCache  *auth.MembershipCache

	OIDC                         *corpauth.OIDCProvider
	Directory                    corpauth.Directory
	WebhookRateLimiter           WebhookRateLimiter
	WebhookIPRateLimiter         WebhookRateLimiter
	WebhookAbsoluteIPRateLimiter WebhookRateLimiter
	WebhookDeliveryWorker        *WebhookDeliveryWorker
	CloudRuntime                 cloudRuntimeProxy

	llmHealth *llmHealthState

	llmHealthHTTPClient *http.Client

	Composio *composio.Service

	ChannelSupervisor *engine.Supervisor

	ChannelRouter *engine.Router

	ChannelMediaReconciler *service.ChannelMediaReconciler

	SlackInstall *slack.InstallService

	SlackBindingTokens *slack.BindingTokenService

	SlackHistory ChatChannelHistoryReader

	LLM *llm.Client

	VCSSecretBox *secretbox.Box

	MCPSecretBox *secretbox.Box

	Audit *audit.Recorder

	PRRefresh *ghsnapshot.Manager

	ProvisioningStore provisioning.PackageStore
	cfg               Config
}

func (h *Handler) DeploymentProfile() deploymentprofile.Profile {
	return h.cfg.DeploymentProfile
}

type EmailSender interface {
	SendVerificationCode(to, code, linkToken, lang string) error
	SendInvitationEmail(to, inviterName, workspaceName, invitationID, lang string) error

	Transport() string
}

func New(queries *db.Queries, txStarter txStarter, hub *realtime.Hub, bus *events.Bus, emailService EmailSender, store storage.Storage, cfSigner *auth.CloudFrontSigner, analyticsClient analytics.Client, cfg Config, daemonHubs ...*daemonws.Hub) *Handler {
	var executor dbExecutor
	if candidate, ok := txStarter.(dbExecutor); ok {
		executor = candidate
	}

	if analyticsClient == nil {
		analyticsClient = analytics.NoopClient{}
	}
	if mode, ok := normalizeAttachmentDownloadMode(cfg.AttachmentDownloadMode); ok {
		cfg.AttachmentDownloadMode = string(mode)
	} else {
		slog.Warn("invalid ATTACHMENT_DOWNLOAD_MODE, using auto", "value", cfg.AttachmentDownloadMode)
		cfg.AttachmentDownloadMode = string(attachmentDownloadModeAuto)
	}
	if cfg.AttachmentDownloadURLTTL <= 0 {
		cfg.AttachmentDownloadURLTTL = defaultAttachmentDownloadURLTTL
	}

	var daemonHub *daemonws.Hub
	if len(daemonHubs) > 0 {
		daemonHub = daemonHubs[0]
	}
	var daemonProfileRefresh RuntimeProfileRefreshNotifier
	var daemonWorkspaceRefresh WorkspaceSetRefreshNotifier
	if daemonHub != nil {
		daemonProfileRefresh = daemonHub
		daemonWorkspaceRefresh = daemonHub
	}

	taskSvc := service.NewTaskService(queries, txStarter, hub, bus, daemonHub)
	taskSvc.Analytics = analyticsClient
	h := &Handler{
		Queries: queries,

		Audit:                        audit.NewRecorder(queries),
		DB:                           executor,
		TxStarter:                    txStarter,
		Hub:                          hub,
		DaemonHub:                    daemonHub,
		DaemonProfileRefresh:         daemonProfileRefresh,
		DaemonWorkspaceRefresh:       daemonWorkspaceRefresh,
		Bus:                          bus,
		TaskService:                  taskSvc,
		IssueService:                 service.NewIssueService(queries, txStarter, bus, analyticsClient, taskSvc),
		AutopilotService:             service.NewAutopilotService(queries, txStarter, bus, taskSvc),
		EmailService:                 emailService,
		UpdateStore:                  NewInMemoryUpdateStore(),
		ModelListStore:               NewInMemoryModelListStore(),
		LocalSkillListStore:          NewInMemoryLocalSkillListStore(),
		LocalSkillImportStore:        NewInMemoryLocalSkillImportStore(),
		LivenessStore:                NewNoopLivenessStore(),
		HeartbeatScheduler:           NewPassthroughHeartbeatScheduler(queries),
		Storage:                      store,
		CFSigner:                     cfSigner,
		Analytics:                    analyticsClient,
		WebhookRateLimiter:           NewMemoryWebhookRateLimiter(DefaultWebhookRateLimit()),
		WebhookIPRateLimiter:         NewMemoryWebhookIPRateLimiter(DefaultWebhookIPRateLimit()),
		WebhookAbsoluteIPRateLimiter: NewMemoryWebhookAbsoluteIPRateLimiter(DefaultWebhookAbsoluteIPRateLimit()),
		CloudRuntime: cloudruntime.NewClient(cloudruntime.Config{
			BaseURL: cfg.CloudRuntimeFleetURL,
			Timeout: cfg.CloudRuntimeFleetTimeout,
		}),
		LLM: llm.New(llm.Config{
			APIKey:       cfg.LLMAPIKey,
			BaseURL:      cfg.LLMBaseURL,
			DefaultModel: cfg.LLMDefaultModel,
		}),
		cfg: cfg,
	}
	h.WebhookDeliveryWorker = NewWebhookDeliveryWorker(h)

	ghClient, err := ghsnapshot.NewClientFromEnv()
	if err != nil {

		slog.Warn("github: PR snapshot pipeline disabled (invalid App private key)", "err", err)
	}
	h.PRRefresh = ghsnapshot.NewManager(ghClient, queries, txStarter, h.broadcastPRSnapshotApplied)

	return h
}

func writeJSON(w http.ResponseWriter, status int, v any) {

	body, err := json.Marshal(v)
	if err != nil {

		body = []byte(`{"error":"failed to encode response"}`)
		status = http.StatusInternalServerError
	}

	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeMeasuredJSON(w http.ResponseWriter, status int, v any) (int, error) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return 0, err
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return len(body), err
	}
	return len(body), nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	body := map[string]string{"error": msg}
	if rid := w.Header().Get("X-Request-ID"); rid != "" {
		body["request_id"] = rid
	}
	writeJSON(w, status, body)
}

func parseUUID(s string) pgtype.UUID                { return util.MustParseUUID(s) }
func uuidToString(u pgtype.UUID) string             { return util.UUIDToString(u) }
func textToPtr(t pgtype.Text) *string               { return util.TextToPtr(t) }
func ptrToText(s *string) pgtype.Text               { return util.PtrToText(s) }
func strToText(s string) pgtype.Text                { return util.StrToText(s) }
func timestampToString(t pgtype.Timestamptz) string { return util.TimestampToString(t) }
func timestampToPtr(t pgtype.Timestamptz) *string   { return util.TimestampToPtr(t) }
func dateToPtr(d pgtype.Date) *string               { return util.DateToPtr(d) }
func uuidToPtr(u pgtype.UUID) *string               { return util.UUIDToPtr(u) }

func uuidsToStrings(us []pgtype.UUID) []string {
	if len(us) == 0 {
		return nil
	}
	out := make([]string, 0, len(us))
	for _, u := range us {
		if u.Valid {
			out = append(out, uuidToString(u))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func uuidStringsOrEmpty(us []pgtype.UUID) []string {
	out := uuidsToStrings(us)
	if out == nil {
		return []string{}
	}
	return out
}

func int8ToPtr(v pgtype.Int8) *int64 { return util.Int8ToPtr(v) }
func int4ToPtr(v pgtype.Int4) *int32 { return util.Int4ToPtr(v) }
func ptrToInt4(v *int32) pgtype.Int4 { return util.PtrToInt4(v) }

func parseUUIDOrBadRequest(w http.ResponseWriter, s, fieldName string) (pgtype.UUID, bool) {
	u, err := util.ParseUUID(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+fieldName)
		return pgtype.UUID{}, false
	}
	return u, true
}

func parseUUIDSliceOrBadRequest(w http.ResponseWriter, ids []string, fieldName string) ([]pgtype.UUID, bool) {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		u, err := util.ParseUUID(id)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+fieldName)
			return nil, false
		}
		uuids[i] = u
	}
	return uuids, true
}

func (h *Handler) publish(eventType, workspaceID, actorType, actorID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

func (h *Handler) notifyDaemonWorkspacesChanged(userIDs ...string) {
	if h.DaemonWorkspaceRefresh == nil {
		return
	}
	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		h.DaemonWorkspaceRefresh.NotifyWorkspacesChanged(userID)
	}
}

func (h *Handler) publishTask(eventType, workspaceID, actorType, actorID, taskID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		TaskID:      taskID,
		Payload:     payload,
	})
}

func (h *Handler) publishChat(eventType, workspaceID, actorType, actorID, chatSessionID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:          eventType,
		WorkspaceID:   workspaceID,
		ActorType:     actorType,
		ActorID:       actorID,
		ChatSessionID: chatSessionID,
		Payload:       payload,
	})
}

func isNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func requestUserID(r *http.Request) string {
	return r.Header.Get("X-User-ID")
}

func (h *Handler) resolveActor(r *http.Request, userID, workspaceID string) (actorType, actorID string) {
	if r.Header.Get("X-Actor-Source") == "task_token" {

		return "agent", r.Header.Get("X-Agent-ID")
	}
	return "member", userID
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func (h *Handler) resolveWorkspaceID(r *http.Request) string {
	return middleware.ResolveWorkspaceIDFromRequest(r, h.Queries)
}

func ctxMember(ctx context.Context) (db.Member, bool) {
	return middleware.MemberFromContext(ctx)
}

func ctxWorkspaceID(ctx context.Context) string {
	return middleware.WorkspaceIDFromContext(ctx)
}

func workspaceIDFromURL(r *http.Request, param string) string {
	if id := middleware.WorkspaceIDFromContext(r.Context()); id != "" {
		return id
	}
	return chi.URLParam(r, param)
}

func (h *Handler) workspaceMember(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	if m, ok := ctxMember(r.Context()); ok {
		return m, true
	}
	return h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
}

func roleAllowed(role string, roles ...string) bool {
	for _, candidate := range roles {
		if role == candidate {
			return true
		}
	}
	return false
}

func countOwners(members []db.Member) int {
	owners := 0
	for _, member := range members {
		if member.Role == "owner" {
			owners++
		}
	}
	return owners
}

func (h *Handler) getWorkspaceMember(ctx context.Context, userID, workspaceID string) (db.Member, error) {
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return db.Member{}, err
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Member{}, err
	}
	return h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: wsUUID,
	})
}

func (h *Handler) requireWorkspaceMember(w http.ResponseWriter, r *http.Request, workspaceID, notFoundMsg string) (db.Member, bool) {
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Member{}, false
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Member{}, false
	}

	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {

		_, userParseErr := util.ParseUUID(userID)
		_, wsParseErr := util.ParseUUID(workspaceID)
		if errors.Is(err, pgx.ErrNoRows) || userParseErr != nil || wsParseErr != nil {
			writeError(w, http.StatusNotFound, notFoundMsg)
			return db.Member{}, false
		}
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "workspace lookup temporarily unavailable")
		return db.Member{}, false
	}

	return member, true
}

func (h *Handler) requireWorkspaceRole(w http.ResponseWriter, r *http.Request, workspaceID, notFoundMsg string, roles ...string) (db.Member, bool) {
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, notFoundMsg)
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, roles...) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, false
	}
	return member, true
}

func (h *Handler) isWorkspaceEntity(ctx context.Context, userType, userID, workspaceID string) bool {
	switch userType {
	case "member":
		_, err := h.getWorkspaceMember(ctx, userID, workspaceID)
		return err == nil
	case "agent":
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			return false
		}
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			return false
		}
		_, err = h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          userUUID,
			WorkspaceID: wsUUID,
		})
		return err == nil
	default:
		return false
	}
}

func (h *Handler) loadIssueForUser(w http.ResponseWriter, r *http.Request, issueID string) (db.Issue, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.Issue{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Issue{}, false
	}

	if issue, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		return issue, true
	}

	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {

		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

func (h *Handler) resolveIssueByIdentifier(ctx context.Context, id, workspaceID string) (db.Issue, bool) {
	parts := splitIdentifier(id)
	if parts == nil {
		return db.Issue{}, false
	}
	if workspaceID == "" {
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: wsUUID,
		Number:      parts.number,
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}

type identifierParts struct {
	prefix string
	number int32
}

func splitIdentifier(id string) *identifierParts {
	idx := -1
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '-' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx >= len(id)-1 {
		return nil
	}
	numStr := id[idx+1:]
	num := 0
	for _, c := range numStr {
		if c < '0' || c > '9' {
			return nil
		}
		num = num*10 + int(c-'0')
	}
	if num <= 0 {
		return nil
	}
	return &identifierParts{prefix: id[:idx], number: int32(num)}
}

func (h *Handler) getIssuePrefix(ctx context.Context, workspaceID pgtype.UUID) string {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return generateIssuePrefix(ws.Name)
}

func (h *Handler) loadAgentForUser(w http.ResponseWriter, r *http.Request, agentID string) (db.Agent, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.Agent{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Agent{}, false
	}

	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent id")
	if !ok {
		return db.Agent{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Agent{}, false
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.Agent{}, false
	}
	if agent.Kind != "user" {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.Agent{}, false
	}
	return agent, true
}

func (h *Handler) loadInboxItemForUser(w http.ResponseWriter, r *http.Request, itemID string) (db.InboxItem, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.InboxItem{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.InboxItem{}, false
	}

	itemUUID, ok := parseUUIDOrBadRequest(w, itemID, "inbox item id")
	if !ok {
		return db.InboxItem{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.InboxItem{}, false
	}

	item, err := h.Queries.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          itemUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return db.InboxItem{}, false
	}

	if item.RecipientType != "member" || uuidToString(item.RecipientID) != userID {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return db.InboxItem{}, false
	}
	return item, true
}
