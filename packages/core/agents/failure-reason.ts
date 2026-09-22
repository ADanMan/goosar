import type { TaskFailureReason } from '../types';

export function resolveFailureReasonKey<K extends string>(
  reason: TaskFailureReason | string | null | undefined,
  copy: Readonly<Partial<Record<K, unknown>>>,
): K | undefined {
  if (!reason) return undefined;
  if (Object.prototype.hasOwnProperty.call(copy, reason)) {
    return reason as K;
  }
  const separator = reason.indexOf('.');
  if (separator > 0) {
    const family = reason.slice(0, separator);
    if (Object.prototype.hasOwnProperty.call(copy, family)) {
      return family as K;
    }
  }
  return undefined;
}
