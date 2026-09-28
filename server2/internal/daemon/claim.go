// claim.go — POST /api/daemon/runtimes/{runtimeId}/tasks/claim и
// POST /api/daemon/tasks/claim (+ алиас /api/daemon/claim): захват задач из
// очереди dispatch_jobs (internal/dispatch.Store.ClaimNext делает саму
// SKIP LOCKED работу — здесь только протокольная обвязка: минт mat_-токена,
// сборка AgentTask, публикация task:dispatch).
package daemon

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
)

// claimOne захватывает не более одной задачи рантайма executorID.
// found=false соответствует пустой очереди (contract: `{"task":null}`).
func (d *Deps) claimOne(ctx context.Context, executorID string) (map[string]any, bool, error) {
	secret, digest, tokenErr := newTaskToken()
	if tokenErr != nil {
		return nil, false, tokenErr
	}
	job, claimed, claimErr := d.Dispatch.Store.ClaimNext(ctx, d.DB.Pool, executorID, digest)
	if claimErr != nil || !claimed {
		return nil, false, claimErr
	}
	task, assembled, buildErr := d.buildAgentTask(ctx, job.ID, secret)
	if buildErr != nil || !assembled {
		return nil, false, buildErr
	}
	d.publish(job.WorkspaceID, "task:dispatch", dispatchEventPayload(job, executorID))
	return task, true, nil
}

// handleClaimForRuntime — POST /api/daemon/runtimes/{runtimeId}/tasks/claim.
func (d *Deps) handleClaimForRuntime(w http.ResponseWriter, r *http.Request) {
	if !d.requireMinDaemonVersion(w, r) {
		return
	}
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	task, claimed, err := d.claimOne(r.Context(), ex.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !claimed {
		task = nil
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"task": task})
}

type batchClaimRequest struct {
	DaemonID   string   `json:"daemon_id"`
	RuntimeIDs []string `json:"runtime_ids"`
	MaxTasks   int      `json:"max_tasks"`
}

// claimBatch — общий низ daemonClaimTasksBatch/daemonClaimTasksBatchAlias и
// tasks.claim по WS (contract §2.2: тот же обработчик за обоими путями).
func (d *Deps) claimBatch(ctx context.Context, actor *httpapi.Actor, req batchClaimRequest) ([]map[string]any, int, string) {
	if req.DaemonID == "" || req.MaxTasks < 0 {
		return nil, http.StatusBadRequest, "daemon_id missing, or max_tasks negative"
	}
	if actor.Source == httpapi.SourceDaemon && actor.DaemonID != "" && actor.DaemonID != req.DaemonID {
		return nil, http.StatusForbidden, "daemon_id does not match the authenticated daemon token"
	}
	budget := req.MaxTasks
	collected := make([]map[string]any, 0, budget)
	for _, runtimeID := range req.RuntimeIDs {
		if budget <= 0 {
			break
		}
		if !d.runtimeUsableForBatch(ctx, actor, runtimeID) {
			continue // не прошло авторизацию/не существует — молча исключается из батча
		}
		claimedHere := d.drainRuntimeQueue(ctx, runtimeID, budget)
		collected = append(collected, claimedHere...)
		budget -= len(claimedHere)
	}
	return collected, http.StatusOK, ""
}

// runtimeUsableForBatch — рантайм существует и актору доступен его воркспейс.
func (d *Deps) runtimeUsableForBatch(ctx context.Context, actor *httpapi.Actor, runtimeID string) bool {
	ex, err := d.Runtime.Store.Get(ctx, runtimeID)
	if err != nil {
		return false
	}
	allowed, err := d.hasWorkspaceAccess(ctx, actor, ex.WorkspaceID)
	return err == nil && allowed
}

// drainRuntimeQueue забирает до limit задач подряд с одного рантайма,
// останавливаясь на первой пустой попытке.
func (d *Deps) drainRuntimeQueue(ctx context.Context, runtimeID string, limit int) []map[string]any {
	out := make([]map[string]any, 0, limit)
	for len(out) < limit {
		task, claimed, err := d.claimOne(ctx, runtimeID)
		if err != nil || !claimed {
			return out
		}
		out = append(out, task)
	}
	return out
}

func (d *Deps) handleClaimBatch(w http.ResponseWriter, r *http.Request) {
	if !d.requireMinDaemonVersion(w, r) {
		return
	}
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req batchClaimRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	tasks, status, msg := d.claimBatch(r.Context(), actor, req)
	if status != http.StatusOK {
		httpapi.WriteError(w, status, msg, "")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func dispatchEventPayload(job dispatch.Job, executorID string) map[string]any {
	ev := map[string]any{"task_id": job.ID, "agent_id": job.OperativeID, "runtime_id": executorID}
	if job.TicketID != nil {
		ev["issue_id"] = *job.TicketID
	}
	if job.ConvoID != nil {
		ev["chat_session_id"] = *job.ConvoID
	}
	return ev
}

// --- lease/восстановление уже забранной задачи ------------------------------
//
// Три родственных маршрута вокруг задачи, уже забранной, но ещё не
// стартовавшей: продление lease, восстановление списка после рестарта
// демона, и переоткладывание "осиротевших" задач переподключившегося
// рантайма (contract §3.7).

// handlePrepareLease — POST .../tasks/{taskId}/prepare-lease.
func (d *Deps) handlePrepareLease(w http.ResponseWriter, r *http.Request) {
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	extended, leaseOK, err := d.Dispatch.Store.TouchLease(r.Context(), d.DB.Pool, ex.ID, r.PathValue("taskId"))
	if err != nil {
		d.internalErr(w, err)
		return
	}
	if !leaseOK {
		httpapi.BadRequest(w, "lease cannot be extended (wrong status, expired, etc.)")
		return
	}
	d.respondTask(w, r, extended.ID)
}

// handlePendingByRuntime — GET .../tasks/pending: восстановление состояния
// демона после рестарта.
func (d *Deps) handlePendingByRuntime(w http.ResponseWriter, r *http.Request) {
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	dispatched, err := d.Dispatch.Store.ListDispatchedOrRunning(r.Context(), d.DB.Pool, ex.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, d.assembleTasks(r, dispatched))
}

// assembleTasks собирает AgentTask для каждого запуска, тихо пропуская тот,
// что не удалось собрать (не должно случаться для только что прочитанных
// jobs, но лучше отдать частичный список, чем провалить весь запрос).
func (d *Deps) assembleTasks(r *http.Request, jobs []dispatch.Job) []map[string]any {
	out := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		task, assembled, err := d.buildAgentTask(r.Context(), job.ID, "")
		if err == nil && assembled {
			out = append(out, task)
		}
	}
	return out
}

// handleRecoverOrphans — POST .../recover-orphans.
func (d *Deps) handleRecoverOrphans(w http.ResponseWriter, r *http.Request) {
	_, ex, ok := d.requireRuntimeAccess(w, r)
	if !ok {
		return
	}
	orphaned, retried, err := d.Dispatch.Store.RecoverOrphans(r.Context(), d.DB.Pool, ex.ID)
	if err != nil {
		d.internalErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]int{"orphaned": orphaned, "retried": retried})
}

// --- минимальная версия клиента (RequireMinDaemonVersion, contract §3.7) ---
//
// Claim-ручки требуют X-Client-Version не ниже GOOSAR_MIN_DAEMON_VERSION
// (или встроенного дефолта) — иначе 426 Upgrade Required.

// defaultMinDaemonVersion — встроенный дефолт, когда GOOSAR_MIN_DAEMON_VERSION
// не задан: "0.0.0" принимает любой клиент (контракт не публикует
// конкретное число для clean-room реализации — решение см. decisions.md).
const defaultMinDaemonVersion = "0.0.0"

// parseVersion разбирает "x.y.z" (с любым числом сегментов) в срез int;
// нечисловые/отсутствующие сегменты читаются как 0 — permissive parser,
// клиенту не обязательно слать строгий semver.
func parseVersion(v string) []int {
	segments := strings.Split(strings.TrimPrefix(v, "v"), ".")
	out := make([]int, len(segments))
	for i, seg := range segments {
		n, _ := strconv.Atoi(strings.TrimSpace(seg))
		out[i] = n
	}
	return out
}

// versionAtLeast — a >= b, сравнивая по сегментам слева направо; более
// короткая версия дополняется нулями.
func versionAtLeast(a, b []int) bool {
	longest := len(a)
	if len(b) > longest {
		longest = len(b)
	}
	for i := 0; i < longest; i++ {
		av, bv := segmentAt(a, i), segmentAt(b, i)
		if av != bv {
			return av > bv
		}
	}
	return true
}

func segmentAt(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// requireMinDaemonVersion — false + написанный 426, если X-Client-Version
// клиента ниже минимальной. Пустой заголовок — тоже отклоняется (контракт:
// клиент обязан заявлять версию на claim-путях), кроме случая, когда
// минимальная версия — сам дефолт "0.0.0" (совместимость со старыми
// сборками демона, которые вообще не шлют этот заголовок).
func (d *Deps) requireMinDaemonVersion(w http.ResponseWriter, r *http.Request) bool {
	minVersion := d.Config.MinDaemonVersion
	if minVersion == "" {
		minVersion = defaultMinDaemonVersion
	}
	client := r.Header.Get("X-Client-Version")
	if client == "" && minVersion == defaultMinDaemonVersion {
		return true
	}
	if client == "" || !versionAtLeast(parseVersion(client), parseVersion(minVersion)) {
		httpapi.WriteError(w, http.StatusUpgradeRequired, "daemon too old: upgrade required", "daemon_too_old")
		return false
	}
	return true
}
