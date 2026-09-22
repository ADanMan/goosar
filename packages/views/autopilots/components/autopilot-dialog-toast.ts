import type { TFunction } from 'i18next';

export function formatSchedulePartialFailureToast(
  t: TFunction<'autopilots'>,
  kind: 'create' | 'update',
  reason: string | null,
): string {
  if (reason) {
    return kind === 'create'
      ? t(($) => $.dialog.toast_create_partial_with_reason, { reason })
      : t(($) => $.dialog.toast_update_partial_with_reason, { reason });
  }
  return kind === 'create'
    ? t(($) => $.dialog.toast_create_partial)
    : t(($) => $.dialog.toast_update_partial);
}
