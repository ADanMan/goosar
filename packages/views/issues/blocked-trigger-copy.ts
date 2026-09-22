import type { useT } from '../i18n';

type IssuesT = ReturnType<typeof useT<'issues'>>['t'];

export function blockedReasonLabel(reasonCode: string, t: IssuesT): string {
  switch (reasonCode) {
    case 'invocation_not_allowed':
      return t(($) => $.comment.trigger_blocked_invocation_not_allowed);
    case 'target_unavailable':
      return t(($) => $.comment.trigger_blocked_target_unavailable);
    case 'runtime_offline':
      return t(($) => $.comment.trigger_blocked_runtime_offline);
    default:
      return t(($) => $.comment.trigger_blocked_generic);
  }
}

export function blockedShortReasonLabel(reasonCode: string, t: IssuesT): string {
  switch (reasonCode) {
    case 'invocation_not_allowed':
      return t(($) => $.comment.trigger_blocked_short_invocation_not_allowed);
    case 'target_unavailable':
      return t(($) => $.comment.trigger_blocked_short_target_unavailable);
    case 'runtime_offline':
      return t(($) => $.comment.trigger_blocked_short_runtime_offline);
    default:
      return t(($) => $.comment.trigger_blocked_short_generic);
  }
}
