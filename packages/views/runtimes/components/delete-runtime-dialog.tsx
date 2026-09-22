'use client';

import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, Globe, Info, Lock } from 'lucide-react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import { ApiError } from '@goosar/core/api';
import type { Agent, AgentRuntime, MemberWithUser } from '@goosar/core/types';
import { runtimeDisplayLabel } from '@goosar/core/runtimes';
import {
  useDeleteRuntime,
  useArchiveAgentsAndDeleteRuntime,
} from '@goosar/core/runtimes/mutations';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { type AgentPresenceDetail, useWorkspacePresenceMap } from '@goosar/core/agents';
import { useAuthStore } from '@goosar/core/auth';
import { AlertDialog, AlertDialogContent } from '@goosar/ui/components/ui/alert-dialog';
import { Button } from '@goosar/ui/components/ui/button';
import { Checkbox } from '@goosar/ui/components/ui/checkbox';
import { ActorAvatar } from '../../common/actor-avatar';
import { availabilityConfig, workloadConfig } from '../../agents/presence';
import { useT } from '../../i18n';
import { isSelfHealingRuntime } from '../utils';

export interface DeleteRuntimeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  runtime: AgentRuntime;
  wsId: string;
  onDeleted: () => void;
}

export function DeleteRuntimeDialog({
  open,
  onOpenChange,
  runtime,
  wsId,
  onDeleted,
}: DeleteRuntimeDialogProps) {
  const { t } = useT('runtimes');
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const user = useAuthStore((s) => s.user);

  const cachedActiveAgents = useMemo(
    () => agents.filter((a) => a.runtime_id === runtime.id && !a.archived_at),
    [agents, runtime.id],
  );
  const [planAgents, setPlanAgents] = useState<Agent[]>(cachedActiveAgents);
  const cascade = planAgents.length > 0;

  const [confirmed, setConfirmed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [planChangedNotice, setPlanChangedNotice] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setPlanAgents(cachedActiveAgents);
      setConfirmed(false);
      setSubmitting(false);
      setPlanChangedNotice(null);
    }
  }, [open, cachedActiveAgents]);

  const lightMutation = useDeleteRuntime(wsId);
  const cascadeMutation = useArchiveAgentsAndDeleteRuntime(wsId);

  const handleConfirm = async () => {
    setSubmitting(true);
    setPlanChangedNotice(null);

    try {
      if (cascade) {
        await cascadeMutation.mutateAsync({
          runtimeId: runtime.id,
          expectedActiveAgentIds: planAgents.map((a) => a.id),
        });
        onDeleted();
      } else {
        try {
          await lightMutation.mutateAsync(runtime.id);
          onDeleted();
        } catch (err) {
          const conflict = parseActiveAgentsConflict(err);
          if (conflict?.code === 'runtime_has_active_agents') {
            setPlanAgents(conflict.activeAgents);
            setConfirmed(false);
            setPlanChangedNotice(
              t(($) => $.detail.delete_dialog.cascade.notice_runtime_has_active_agents),
            );
            return;
          }
          throw err;
        }
      }
    } catch (err) {
      const conflict = parseActiveAgentsConflict(err);
      if (conflict?.code === 'runtime_delete_plan_changed') {
        setPlanAgents(conflict.activeAgents);
        setConfirmed(false);
        setPlanChangedNotice(
          t(($) => $.detail.delete_dialog.cascade.notice_runtime_delete_plan_changed),
        );
        return;
      }
      const message =
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.detail.delete_dialog.cascade.delete_failed_toast);
      toast.error(message);
    } finally {
      setSubmitting(false);
    }
  };

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className={
          cascade
            ? 'w-[calc(100vw-2rem)] !max-w-[640px] gap-0 overflow-hidden rounded-lg p-0'
            : 'w-[calc(100vw-2rem)] !max-w-[440px] gap-0 overflow-hidden rounded-lg p-0'
        }
        onClick={(e) => e.stopPropagation()}
      >
        {cascade ? (
          <CascadeBody
            runtime={runtime}
            agents={planAgents}
            members={members}
            presenceMap={presenceMap}
            currentUserId={user?.id ?? null}
            confirmed={confirmed}
            onConfirmedChange={setConfirmed}
            planChangedNotice={planChangedNotice}
            submitting={submitting}
            onCancel={() => handleOpenChange(false)}
            onConfirm={handleConfirm}
          />
        ) : (
          <LightBody
            runtime={runtime}
            submitting={submitting}
            onCancel={() => handleOpenChange(false)}
            onConfirm={handleConfirm}
          />
        )}
      </AlertDialogContent>
    </AlertDialog>
  );
}

function DeletePersistenceNotice({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT('runtimes');
  if (runtime.profile_id) {
    return (
      <div
        role="status"
        className="mt-3 flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-xs"
      >
        <Info className="mt-0.5 size-3.5 shrink-0 text-warning" />
        <span>{t(($) => $.detail.delete_dialog.profile_backed_notice)}</span>
      </div>
    );
  }
  if (!isSelfHealingRuntime(runtime)) return null;
  return (
    <div
      role="status"
      className="mt-3 flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-xs"
    >
      <Info className="mt-0.5 size-3.5 shrink-0 text-warning" />
      <span>{t(($) => $.detail.delete_dialog.self_heal_notice)}</span>
    </div>
  );
}

function LightBody({
  runtime,
  submitting,
  onCancel,
  onConfirm,
}: {
  runtime: AgentRuntime;
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT('runtimes');
  return (
    <>
      <div className="px-5 pb-4 pt-5">
        <h2 className="text-base font-semibold">{t(($) => $.detail.delete_dialog.light.title)}</h2>
        <p className="mt-1 text-sm leading-5 text-muted-foreground">
          {t(($) => $.detail.delete_dialog.light.description, {
            name: runtimeDisplayLabel(runtime),
          })}
        </p>
        <DeletePersistenceNotice runtime={runtime} />
      </div>
      <div className="border-t bg-muted/25 px-5 py-3">
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button
            type="button"
            variant="outline"
            className="w-full sm:w-auto"
            onClick={onCancel}
            disabled={submitting}
          >
            {t(($) => $.detail.delete_dialog.light.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            className="w-full sm:w-auto"
            onClick={onConfirm}
            disabled={submitting}
          >
            {submitting
              ? t(($) => $.detail.delete_dialog.light.submitting)
              : t(($) => $.detail.delete_dialog.light.confirm)}
          </Button>
        </div>
      </div>
    </>
  );
}

function CascadeBody({
  runtime,
  agents,
  members,
  presenceMap,
  currentUserId,
  confirmed,
  onConfirmedChange,
  planChangedNotice,
  submitting,
  onCancel,
  onConfirm,
}: {
  runtime: AgentRuntime;
  agents: Agent[];
  members: MemberWithUser[];
  presenceMap: Map<string, AgentPresenceDetail>;
  currentUserId: string | null;
  confirmed: boolean;
  onConfirmedChange: (next: boolean) => void;
  planChangedNotice: string | null;
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT('runtimes');
  const count = agents.length;

  return (
    <>
      <div className="px-5 pb-4 pt-5">
        <h2 className="text-base font-semibold">
          {t(($) => $.detail.delete_dialog.cascade.title, { count })}
        </h2>
        <p className="mt-1 text-sm leading-5 text-muted-foreground">
          {t(($) => $.detail.delete_dialog.cascade.description, {
            name: runtimeDisplayLabel(runtime),
          })}
        </p>

        <DeletePersistenceNotice runtime={runtime} />

        {/* Destructive banner — keep the user's eye on the irreversible
            half before they scan the agent table. */}
        <div
          role="alert"
          className="mt-3 flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive"
        >
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          <span>{t(($) => $.detail.delete_dialog.cascade.warning)}</span>
        </div>

        {planChangedNotice && (
          <div
            role="status"
            className="mt-2 rounded-md border bg-muted/40 px-3 py-2 text-xs text-foreground"
          >
            {planChangedNotice}
          </div>
        )}

        <AgentPlanTable
          agents={agents}
          members={members}
          presenceMap={presenceMap}
          currentUserId={currentUserId}
        />
      </div>

      <div className="border-t bg-muted/25 px-5 py-4">
        <label className="flex cursor-pointer items-start gap-2 text-sm text-foreground">
          <Checkbox
            className="mt-0.5"
            checked={confirmed}
            onCheckedChange={(next) => onConfirmedChange(next === true)}
            disabled={submitting}
          />
          <span className="leading-5">
            {t(($) => $.detail.delete_dialog.cascade.checkbox, { count })}
          </span>
        </label>
        <div className="mt-3 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button
            type="button"
            variant="outline"
            className="w-full sm:w-auto"
            onClick={onCancel}
            disabled={submitting}
          >
            {t(($) => $.detail.delete_dialog.cascade.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            className="w-full sm:w-auto"
            onClick={onConfirm}
            disabled={!confirmed || submitting}
          >
            {submitting
              ? t(($) => $.detail.delete_dialog.cascade.submitting)
              : t(($) => $.detail.delete_dialog.cascade.confirm, { count })}
          </Button>
        </div>
      </div>
    </>
  );
}

function AgentPlanTable({
  agents,
  members,
  presenceMap,
  currentUserId,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  presenceMap: Map<string, AgentPresenceDetail>;
  currentUserId: string | null;
}) {
  const { t } = useT('runtimes');
  const memberById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  return (
    <div className="mt-3 overflow-hidden rounded-md border">
      <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] gap-3 border-b bg-muted/40 px-3 py-2 text-[11px] uppercase tracking-wide text-muted-foreground">
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_agent)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_owner)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_status)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_visibility)}</span>
        <span>{t(($) => $.detail.delete_dialog.cascade.table.header_model)}</span>
      </div>
      <div className="max-h-[240px] overflow-y-auto divide-y">
        {agents.map((agent) => {
          const ownerMember = agent.owner_id ? (memberById.get(agent.owner_id) ?? null) : null;
          const ownerLabel = ownerMember
            ? ownerMember.user_id === currentUserId
              ? t(($) => $.detail.delete_dialog.cascade.table.owner_self)
              : ownerMember.name
            : t(($) => $.detail.delete_dialog.cascade.table.owner_unassigned);
          const presence = presenceMap.get(agent.id);
          return (
            <div
              key={agent.id}
              className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)] items-center gap-3 px-3 py-2 text-xs"
            >
              <span className="inline-flex min-w-0 items-center gap-2">
                <ActorAvatar actorType="agent" actorId={agent.id} size="sm" enableHoverCard />
                <span className="truncate font-medium text-foreground">{agent.name}</span>
              </span>
              <span className="inline-flex min-w-0 items-center gap-1.5">
                {ownerMember ? (
                  <ActorAvatar actorType="member" actorId={ownerMember.user_id} size="sm" />
                ) : null}
                <span className="truncate text-muted-foreground">{ownerLabel}</span>
              </span>
              <PresenceCell presence={presence} />
              <VisibilityCell visibility={agent.visibility} />
              <span className="truncate text-muted-foreground">
                {agent.model || t(($) => $.detail.delete_dialog.cascade.table.model_unset)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PresenceCell({ presence }: { presence: AgentPresenceDetail | undefined }) {
  const { t } = useT('runtimes');
  if (!presence) {
    return (
      <span className="text-muted-foreground/60">
        {t(($) => $.detail.delete_dialog.cascade.table.presence_unknown)}
      </span>
    );
  }
  const av = availabilityConfig[presence.availability];
  const wl = workloadConfig[presence.workload];
  const counts =
    presence.workload === 'working'
      ? presence.queuedCount > 0
        ? `${presence.runningCount} +${presence.queuedCount}q`
        : `${presence.runningCount}`
      : presence.workload === 'queued'
        ? `${presence.queuedCount}`
        : null;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className={`size-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      <span className={av.textClass}>
        {presence.workload === 'idle'
          ? t(($) => $.detail.delete_dialog.cascade.table.workload_idle)
          : null}
        {presence.workload === 'working' && (
          <wl.icon className={`mr-1 inline size-3 align-[-2px] animate-spin ${wl.textClass}`} />
        )}
        {presence.workload === 'queued' && (
          <wl.icon className={`mr-1 inline size-3 align-[-2px] ${wl.textClass}`} />
        )}
        {presence.workload === 'working' &&
          t(($) => $.detail.delete_dialog.cascade.table.workload_working)}
        {presence.workload === 'queued' &&
          t(($) => $.detail.delete_dialog.cascade.table.workload_queued)}
      </span>
      {counts && <span className="font-mono tabular-nums text-muted-foreground/80">{counts}</span>}
    </span>
  );
}

function VisibilityCell({ visibility }: { visibility: string }) {
  const { t } = useT('runtimes');
  if (visibility === 'public' || visibility === 'workspace') {
    return (
      <span className="inline-flex items-center gap-1 text-muted-foreground">
        <Globe className="size-3" />
        <span>{t(($) => $.detail.delete_dialog.cascade.table.visibility_workspace)}</span>
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground">
      <Lock className="size-3" />
      <span>{t(($) => $.detail.delete_dialog.cascade.table.visibility_private)}</span>
    </span>
  );
}

interface ActiveAgentsConflict {
  code: 'runtime_has_active_agents' | 'runtime_delete_plan_changed';
  activeAgents: Agent[];
}

function parseActiveAgentsConflict(err: unknown): ActiveAgentsConflict | null {
  if (!(err instanceof ApiError)) return null;
  if (err.status !== 409) return null;
  const body = err.body;
  if (!body || typeof body !== 'object') return null;
  const code = (body as Record<string, unknown>).code;
  if (code !== 'runtime_has_active_agents' && code !== 'runtime_delete_plan_changed') {
    return null;
  }
  const rawAgents = (body as Record<string, unknown>).active_agents;
  if (!Array.isArray(rawAgents)) {
    return { code, activeAgents: [] };
  }
  const activeAgents = rawAgents.filter(
    (a): a is Agent =>
      typeof a === 'object' &&
      a !== null &&
      typeof (a as Record<string, unknown>).id === 'string' &&
      typeof (a as Record<string, unknown>).name === 'string',
  );
  return { code, activeAgents };
}
