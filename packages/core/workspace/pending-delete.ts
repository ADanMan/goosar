/**
 * Реестр id workspace, чьё удаление инициировано ЭТИМ клиентом.
 *
 * Помечается в useDeleteWorkspace.onMutate. Realtime-обработчик
 * `workspace:deleted` проверяет реестр и ничего не делает для
 * самоинициированных удалений: очистку storage и навигацию ведёт сам поток
 * мутации, чтобы не race'иться с обработчиком.
 *
 * Снимается только при ошибке DELETE — workspace всё ещё существует, и
 * последующее внешнее удаление того же id должно обработаться как обычно.
 * При успехе id остаётся помеченным навсегда, подавляя эхо своего же
 * удаления через WS.
 *
 * Хранится на уровне модуля, а не React-состояния, потому что realtime-
 * обработчик работает вне дерева компонентов. По построению — на вкладку:
 * у других вкладок/устройств реестр пуст, и их обработчики срабатывают как
 * обычно.
 */
const pendingDeletes = new Set<string>();

export function markWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.add(workspaceId);
}

export function unmarkWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.delete(workspaceId);
}

export function isWorkspaceDeletePending(workspaceId: string): boolean {
  return pendingDeletes.has(workspaceId);
}
