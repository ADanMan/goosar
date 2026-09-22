import type { QueryClient } from '@tanstack/react-query';
import { inboxKeys } from './queries';
import type { InboxItem, IssueStatus } from '../types';

export function onInboxNew(qc: QueryClient, wsId: string, _item: InboxItem) {
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

export function patchInboxIssueStatus(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  const patch = (old: InboxItem[] | undefined) =>
    old?.map((i) => (i.issue_id === issueId ? { ...i, issue_status: status } : i));
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), patch);
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), patch);
}

export function onInboxIssueStatusChanged(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  patchInboxIssueStatus(qc, wsId, issueId, status);
}

export function onInboxIssueDeleted(qc: QueryClient, wsId: string, issueId: string) {
  const drop = (old: InboxItem[] | undefined) => old?.filter((i) => i.issue_id !== issueId);
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), drop);
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), drop);
}

export function onInboxInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

export function onInboxSummaryInvalidate(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}
