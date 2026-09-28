// Правка T-028: два небезопасных для человека типа актора из contract §1.1/
// §1.3, которые до этой сессии authn всегда отклонял (см. decisions.md,
// раздел T-027 «Пробелы спецификации», п. 5).
//
// `mat_...` — токен агента-исполнителя задачи, выдаётся демону при claim
// (contract §3.7 "auth_token"). Сам authn не знает, что такое dispatch_jobs —
// проверку конкретного секрета делает TaskActorLookup, которую при сборке
// сервера подключает домен daemon (владелец claim/complete/fail); authn
// только резолвит префикс токена и кладёт результат в httpapi.Actor.
//
// `mdt_...` — токен демона, несущий workspace_id/daemon_id прямо в себе ("не
// требует похода в БД при попадании в кэш"). Ни один маршрут контракта не
// выдаёт такой токен клиенту (см. decisions.md, раздел T-028): реальный
// CLI-демон аутентифицируется на /api/daemon/** персональным токеном
// (`gsl_...`, тем же самым, что и обычный пользователь — §1.3 прямо
// перечисляет его в цепочке daemonAuth), поэтому это — самопроверяемый (без
// обращения к БД, HMAC на JWT_SECRET процесса) формат на случай, если токен
// всё же где-то предъявлен, а не заглушка "всегда отклонить".
package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

const daemonTokenPrefix = "mdt_"

// MintDaemonToken строит mdt_-токен для (workspaceID, daemonID). Не вызывается
// ни одним HTTP-обработчиком контракта (см. комментарий пакета) — оставлена
// как единственная точка, которой воспользуется будущий выпускающий маршрут
// или административный инструмент, вместо дублирования формата в двух местах.
func MintDaemonToken(secret, workspaceID, daemonID string) string {
	payload := workspaceID + "|" + daemonID
	sig := hmac.New(sha256.New, []byte(secret))
	sig.Write([]byte(payload))
	mac := base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	return daemonTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + mac
}

// verifyDaemonToken разбирает и проверяет подпись; ok=false на любую
// нестыковку формата или подписи (истечения срока у этого формата нет —
// он привязан к workspace/daemon_id, а не ко времени, ровно как описывает
// контракт).
func verifyDaemonToken(secret, token string) (workspaceID, daemonID string, ok bool) {
	rest, found := strings.CutPrefix(token, daemonTokenPrefix)
	if !found {
		return "", "", false
	}
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	sig := hmac.New(sha256.New, []byte(secret))
	sig.Write(payloadRaw)
	want := base64.RawURLEncoding.EncodeToString(sig.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[1])) != 1 {
		return "", "", false
	}
	payload := strings.SplitN(string(payloadRaw), "|", 2)
	if len(payload) != 2 || payload[0] == "" || payload[1] == "" {
		return "", "", false
	}
	return payload[0], payload[1], true
}

func (d *Deps) actorFromDaemonToken(token string) (*httpapi.Actor, bool) {
	workspaceID, daemonID, ok := verifyDaemonToken(d.Config.JWTSecret, token)
	if !ok {
		return nil, false
	}
	return &httpapi.Actor{
		IsHuman:         false,
		Source:          httpapi.SourceDaemon,
		DaemonID:        daemonID,
		TaskWorkspaceID: workspaceID,
	}, true
}

// --- mat_-токен агента-исполнителя задачи ------------------------------------

// TaskActor — то немногое, что нужно контексту запроса от найденного по
// mat_-токену запуска задачи: воркспейс (для резолва §1.4, приоритет
// task_token) и агент, от чьего имени сейчас действует демон.
type TaskActor struct {
	WorkspaceID string
	AgentID     string
	JobID       string
}

// TaskActorLookup проверяет токен агента-исполнителя задачи. Реализация
// живёт в домене daemon (там же, где выдаётся сам токен при claim и
// отзывается при complete/fail) — authn получает её через
// SetTaskActorLookup, чтобы не импортировать dispatch_jobs напрямую.
type TaskActorLookup interface {
	Lookup(ctx context.Context, token string) (TaskActor, bool, error)
}

// SetTaskActorLookup подключает проверку mat_-токенов; вызывается один раз
// при сборке Deps (см. internal/app/deps.go), тем же приёмом, что и
// note.Deps.SetDispatcher — маленький сеттер поверх уже собранного Deps,
// а не изменение сигнатуры New().
func (d *Deps) SetTaskActorLookup(l TaskActorLookup) { d.TaskActors = l }

func (d *Deps) actorFromTaskToken(ctx context.Context, token string) (*httpapi.Actor, bool) {
	if d.TaskActors == nil {
		return nil, false
	}
	ta, ok, err := d.TaskActors.Lookup(ctx, token)
	if err != nil || !ok {
		return nil, false
	}
	return &httpapi.Actor{
		UserID:          ta.AgentID,
		IsHuman:         false,
		Source:          httpapi.SourceTaskToken,
		TaskWorkspaceID: ta.WorkspaceID,
	}, true
}
