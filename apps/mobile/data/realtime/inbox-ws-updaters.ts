// Патчеры WS-кэша входящих. Зеркалят packages/core/inbox/ws-updaters.ts.
import type { QueryClient } from "@tanstack/react-query";
import type { InboxItem, IssueStatus } from "@goosar/core/types";
import { inboxKeys } from "@/data/queries/inbox";

export function patchInboxIssueStatus(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  status: IssueStatus,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.map((i) =>
      i.issue_id === issueId ? { ...i, issue_status: status } : i,
    ),
  );
}

export function dropInboxItemsByIssue(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.filter((i) => i.issue_id !== issueId),
  );
}
