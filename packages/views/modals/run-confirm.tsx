'use client';

import { useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { Button } from '@goosar/ui/components/ui/button';
import { Textarea } from '@goosar/ui/components/ui/textarea';
import { Spinner } from '@goosar/ui/components/ui/spinner';
import type { IssueAssigneeType, UpdateIssueRequest } from '@goosar/core/types';
import { useUpdateIssue, useBatchUpdateIssues } from '@goosar/core/issues/mutations';
import { useActorName } from '@goosar/core/workspace/hooks';
import { useWorkspaceId } from '@goosar/core/hooks';
import { agentListOptions, squadListOptions } from '@goosar/core/workspace/queries';
import { runtimeListOptions, readRuntimeCliVersion, handoffSupported } from '@goosar/core/runtimes';
import { useT } from '../i18n';

const MAX_HANDOFF_NOTE = 2000;

const NAME_FENCE = '\u0000';

function boldName(text: string): ReactNode {
  const parts = text.split(NAME_FENCE);
  if (parts.length !== 3) return text;
  return (
    <>
      {parts[0]}
      <span className="font-semibold text-foreground">{parts[1]}</span>
      {parts[2]}
    </>
  );
}

interface RunConfirmData {
  issueIds?: string[];
  mode?: 'assign';
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  assigneeName?: string;
}

export function RunConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT('modals');
  const { getActorName } = useActorName();
  const d = (data ?? {}) as RunConfirmData;
  const issueIds = d.issueIds ?? [];

  const [note, setNote] = useState('');
  const [pendingAction, setPendingAction] = useState<'go' | 'suppress' | null>(null);
  const submitting = pendingAction !== null;

  const updateIssue = useUpdateIssue();
  const batchUpdate = useBatchUpdateIssues();

  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  const { data: squads = [] } = useQuery({ ...squadListOptions(wsId), enabled: !!wsId });
  const localHandoff = useMemo<boolean | null>(() => {
    if (!d.assigneeId) return null;
    let agentId: string | undefined;
    if (d.assigneeType === 'agent') {
      agentId = d.assigneeId;
    } else if (d.assigneeType === 'squad') {
      agentId = squads.find((s) => s.id === d.assigneeId)?.leader_id;
    }
    if (!agentId) return null;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent?.runtime_id) return null;
    const runtime = runtimes.find((r) => r.id === agent.runtime_id);
    if (!runtime) return null;
    return handoffSupported(readRuntimeCliVersion(runtime.metadata));
  }, [d.assigneeType, d.assigneeId, agents, runtimes, squads]);

  const noteDisabled = localHandoff === false;

  const applyTo = (extra: Partial<UpdateIssueRequest>) => {
    const base: UpdateIssueRequest = {
      assignee_type: d.assigneeType ?? null,
      assignee_id: d.assigneeId ?? null,
    };
    return { ...base, ...extra };
  };

  const assigneeName =
    d.assigneeName ??
    getActorName(d.assigneeType === 'squad' ? 'squad' : 'agent', d.assigneeId ?? '');

  const submit = async (suppressRun: boolean) => {
    if (issueIds.length === 0 || submitting) return;
    setPendingAction(suppressRun ? 'suppress' : 'go');
    const payload = applyTo({
      ...(suppressRun ? { suppress_run: true } : {}),
      ...(!suppressRun && !noteDisabled && note.trim() ? { handoff_note: note.trim() } : {}),
    });
    try {
      if (issueIds.length === 1) {
        await updateIssue.mutateAsync({ id: issueIds[0]!, ...payload });
      } else {
        await batchUpdate.mutateAsync({ ids: issueIds, updates: payload });
      }
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.run_confirm.toast_failed),
      );
      setPendingAction(null);
    }
  };

  const headline: ReactNode = boldName(
    issueIds.length > 1
      ? t(($) => $.run_confirm.assign_batch, {
          name: `${NAME_FENCE}${assigneeName}${NAME_FENCE}`,
          count: issueIds.length,
        })
      : t(($) => $.run_confirm.assign_single, {
          name: `${NAME_FENCE}${assigneeName}${NAME_FENCE}`,
        }),
  );

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v && !submitting) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.run_confirm.title_assign)}</DialogTitle>
          <DialogDescription>{headline}</DialogDescription>
        </DialogHeader>

        {/* Always mounted and always usable on the first frame — nothing about
            this box depends on a server answer. */}
        <div className="grid gap-1.5">
          <label className="text-sm font-medium" htmlFor="handoff-note">
            {t(($) => $.run_confirm.note_label)}
          </label>
          <Textarea
            id="handoff-note"
            value={note}
            maxLength={MAX_HANDOFF_NOTE}
            disabled={submitting || noteDisabled}
            placeholder={t(($) => $.run_confirm.note_placeholder)}
            onChange={(e) => setNote(e.target.value)}
            rows={3}
          />
          {noteDisabled ? (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.run_confirm.note_unsupported)}
            </p>
          ) : null}
        </div>

        {/* The only spinner left is on the button the user just pressed, and it
            reflects the write in flight — never a pre-flight check. */}
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={submitting}
            onClick={() => submit(true)}
          >
            {pendingAction === 'suppress' ? (
              <Spinner className="size-4" />
            ) : (
              t(($) => $.run_confirm.dont_start)
            )}
          </Button>
          <Button type="button" disabled={submitting} onClick={() => submit(false)}>
            {pendingAction === 'go' ? (
              <Spinner className="size-4" />
            ) : (
              t(($) => $.run_confirm.confirm_assign)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
