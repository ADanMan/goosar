// Package daemon реализует §2.2/§3.7 контракта — daemon-протокол:
// /api/daemon/register, /deregister, /heartbeat, /ws (RPC + push),
// /workspaces(/repos|/runtime-profiles), захват задач (claim, REST и
// tasks.claim по WS), prepare-lease, skill-bundles/resolve, весь жизненный
// цикл одной задачи (status/start/wait-local-directory/progress/complete/
// fail/usage/messages/cancel-ack/session), gc-check (issues/chat-sessions/
// autopilot-runs/tasks), recover-orphans и отчёты об асинхронных заявках
// (update/models/local-skills/local-skills-import result).
//
// Слой ниже — server2/internal/runtime (executors/executor_probes) и
// server2/internal/dispatch (dispatch_jobs/messages/usage): daemon сам не
// хранит состояние, только строит HTTP/WS-протокол поверх их Store.
package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/authn"
	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/runtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена daemon.
type Deps struct {
	DB        *store.Store
	Runtime   *runtime.Deps
	Dispatch  *dispatch.Deps
	Workspace *workspace.Store // только чтение: репозитории/runtime-профили воркспейса
	Publisher realtime.Publisher
	Config    config.Config
	WSResolve *wsctx.Resolver
	Logger    *slog.Logger

	ws *wsRegistry
}

func New(db *store.Store, runtimeDeps *runtime.Deps, dispatchDeps *dispatch.Deps, workspaceStore *workspace.Store, pub realtime.Publisher, cfg config.Config, logger *slog.Logger) *Deps {
	return &Deps{
		DB:        db,
		Runtime:   runtimeDeps,
		Dispatch:  dispatchDeps,
		Workspace: workspaceStore,
		Publisher: pub,
		Config:    cfg,
		WSResolve: wsctx.New(db),
		Logger:    logger,
		ws:        newWSRegistry(),
	}
}

// TaskActorLookup — см. internal/authn.TaskActorLookup: реализуется здесь,
// потому что только daemon знает формат/хранилище mat_-токена
// (dispatch_jobs.dj_claim_secret_digest). Подключается в internal/app/deps.go
// через authn.Deps.SetTaskActorLookup(daemonDeps).
var _ authn.TaskActorLookup = (*Deps)(nil)

func (d *Deps) publish(workspaceID, eventType string, payload any) {
	if d.Publisher == nil || workspaceID == "" {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{Type: eventType, Payload: payload})
}

// internalErr — общий 500 контракта (`{"error":"internal error","code":"internal_error"}`),
// вынесен в один метод: этот код повторялся дословно на каждой непредвиденной
// ошибке БД во всех обработчиках пакета.
func (d *Deps) internalErr(w http.ResponseWriter, err error) {
	if d.Logger != nil {
		d.Logger.Error("daemon: internal error", "err", err)
	}
	httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
}

func (d *Deps) publishRegisterEvent(workspaceID string, extra map[string]any) {
	payload := map[string]any{}
	for k, v := range extra {
		payload[k] = v
	}
	d.publish(workspaceID, "daemon:register", payload)
}

// --- mat_-токен агента-исполнителя задачи (contract §1.1/§3.7) --------------
//
// Собственная генерация/хэширование, независимая от internal/authn (authn
// только резолвит префикс и делегирует проверку через authn.TaskActorLookup
// сюда, см. Lookup ниже) — тот же принцип, что и у claim-секрета
// dispatch_jobs (dj_claim_secret_digest хранится как отпечаток, не как
// открытый текст).

const (
	taskTokenPrefix    = "mat_"
	taskTokenRandBytes = 24
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// digestOf — необратимый отпечаток секрета (mat_-токена или списка URL
// репозиториев воркспейса, см. handlers_workspaces.go), sha256 в hex.
func digestOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// newTaskToken генерирует свежий mat_-токен вместе с его отпечатком —
// вызывающий (claimOne) сохраняет отпечаток атомарно с переводом строки
// dispatch_jobs в dispatched, а сам токен отдаёт демону в ответе claim.
func newTaskToken() (secret, digest string, err error) {
	entropy := make([]byte, taskTokenRandBytes)
	if _, readErr := rand.Read(entropy); readErr != nil {
		return "", "", fmt.Errorf("daemon: генерация mat_-токена: %w", readErr)
	}
	secret = taskTokenPrefix + base32NoPad.EncodeToString(entropy)
	return secret, digestOf(secret), nil
}

// Lookup реализует authn.TaskActorLookup: находит активный запуск по хэшу
// mat_-токена (см. internal/dispatch.Store.FindByClaimSecretDigest) и
// сообщает authn, от чьего имени сейчас действует демон.
func (d *Deps) Lookup(ctx context.Context, presented string) (authn.TaskActor, bool, error) {
	job, found, lookupErr := d.Dispatch.Store.FindByClaimSecretDigest(ctx, d.DB.Pool, digestOf(presented))
	switch {
	case lookupErr != nil:
		return authn.TaskActor{}, false, lookupErr
	case !found:
		return authn.TaskActor{}, false, nil
	default:
		return authn.TaskActor{WorkspaceID: job.WorkspaceID, AgentID: job.OperativeID, JobID: job.ID}, true, nil
	}
}
