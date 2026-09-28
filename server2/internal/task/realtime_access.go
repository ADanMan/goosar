package task

import "context"

// taskAccessBridge реализует realtime.TaskAccess — узкая щель, через которую
// /ws видит ровно один факт о задачах: существует ли task:<id> (в терминах
// realtime это issue_id — единственный "task-подобный" объект, которым
// сегодня владеет этот пакет) в workspaceID и виден ли он userID. Полный
// список подписчиков/участников задачи контракт не описывает отдельно от
// членства в воркспейсе, поэтому "allowed" здесь эквивалентно "issue
// принадлежит тому же воркспейсу, к которому уже привязан сокет" — сама
// проверка членства в воркспейсе уже прошла на этапе handshake.
type taskAccessBridge struct{ store *Store }

// RealtimeTaskAccess — адаптер для internal/realtime.Register (см.
// internal/app/routes.go).
func (d *Deps) RealtimeTaskAccess() *taskAccessBridge { return &taskAccessBridge{store: d.Store} }

// userID не используется: видимость задачи сегодня определяется только
// членством в воркспейсе (уже проверено на этапе handshake), не ролью или
// персональной подпиской.
func (b *taskAccessBridge) CanAccessTask(ctx context.Context, workspaceID, userID, taskID string) (found, allowed bool, err error) {
	_ = userID
	_, err = b.store.GetIssue(ctx, workspaceID, taskID)
	if err == ErrNotFound {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, true, nil
}
