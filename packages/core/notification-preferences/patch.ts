import type {
  NotificationGroupKey,
  NotificationGroupValue,
  NotificationPreferences,
} from '../types';

const NOTIFICATION_GROUP_KEYS: readonly NotificationGroupKey[] = [
  'assignments',
  'status_changes',
  'comments',
  'updates',
  'agent_activity',
  'system_notifications',
];

function preferenceValue(
  preferences: NotificationPreferences,
  key: NotificationGroupKey,
): NotificationGroupValue {
  return preferences[key] ?? 'all';
}

export function deriveNotificationPreferencePatch(
  previous: NotificationPreferences,
  next: NotificationPreferences,
): NotificationPreferences {
  const patch: NotificationPreferences = {};

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const previousValue = preferenceValue(previous, key);
    const nextValue = preferenceValue(next, key);
    if (previousValue !== nextValue) {
      patch[key] = nextValue;
    }
  }

  return patch;
}

export function applyNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
): NotificationPreferences {
  const next = { ...current };

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const value = patch[key];
    if (value === 'muted') {
      next[key] = value;
    } else if (value === 'all') {
      delete next[key];
    }
  }

  return next;
}

export function rollbackNotificationPreferencePatch(
  current: NotificationPreferences,
  patch: NotificationPreferences,
  previous: NotificationPreferences,
): NotificationPreferences {
  const next = { ...current };

  for (const key of NOTIFICATION_GROUP_KEYS) {
    const patchedValue = patch[key];
    if (patchedValue === undefined || preferenceValue(current, key) !== patchedValue) {
      continue;
    }

    const previousValue = preferenceValue(previous, key);
    if (previousValue === 'all') {
      delete next[key];
    } else {
      next[key] = previousValue;
    }
  }

  return next;
}
