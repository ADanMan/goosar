import type { useT } from '@goosar/views/i18n';

type SettingsT = ReturnType<typeof useT<'settings'>>['t'];

export const KINIT_FAILURE_REASONS = [
  'invalid_principal',
  'wrong_password',
  'principal_unknown',
  'kdc_unreachable',
  'kinit_missing',
  'renew_rejected',
  'unknown',
] as const;

export type KinitFailureReason = (typeof KINIT_FAILURE_REASONS)[number];

function isKnownReason(reason: string): reason is KinitFailureReason {
  return (KINIT_FAILURE_REASONS as readonly string[]).includes(reason);
}

export function kinitFailureMessage(t: SettingsT, reason: string, message: string): string {
  if (!isKnownReason(reason)) {
    return message || t(($) => $.desktop.perimeter.kinit_error.unknown);
  }
  switch (reason) {
    case 'invalid_principal':
      return t(($) => $.desktop.perimeter.kinit_error.invalid_principal);
    case 'wrong_password':
      return t(($) => $.desktop.perimeter.kinit_error.wrong_password);
    case 'principal_unknown':
      return t(($) => $.desktop.perimeter.kinit_error.principal_unknown);
    case 'kdc_unreachable':
      return t(($) => $.desktop.perimeter.kinit_error.kdc_unreachable);
    case 'kinit_missing':
      return t(($) => $.desktop.perimeter.kinit_error.kinit_missing);
    case 'renew_rejected':
      return t(($) => $.desktop.perimeter.kinit_error.renew_rejected);
    case 'unknown':
      return t(($) => $.desktop.perimeter.kinit_error.unknown);
  }
}
