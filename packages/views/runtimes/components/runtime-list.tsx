'use client';

import { useMemo, useState } from 'react';
import {
  AlertTriangle,
  Globe,
  Loader2,
  MonitorOff,
  MoreHorizontal,
  Pencil,
  Trash2,
} from 'lucide-react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import { CurrencyNumberFlow } from '@goosar/ui/components/ui/number-flow';
import type {
  Agent,
  AgentRuntime,
  AgentTask,
  MemberWithUser,
  RuntimeProfile,
} from '@goosar/core/types';
import { useAuthStore } from '@goosar/core/auth';
import { useWorkspaceId } from '@goosar/core/hooks';
import { agentListOptions, memberListOptions } from '@goosar/core/workspace/queries';
import { agentTaskSnapshotOptions } from '@goosar/core/agents';
import {
  deriveRuntimeState,
  runtimeProfileListOptions,
  runtimeUsageOptions,
  type RuntimeAvailability,
} from '@goosar/core/runtimes';
import { useWorkspacePaths } from '@goosar/core/paths';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import {
  ListGrid,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
} from '@goosar/ui/components/ui/list-grid';
import { Tooltip, TooltipContent, TooltipTrigger } from '@goosar/ui/components/ui/tooltip';
import { useRowLink } from '../../navigation';
import { ActorAvatar } from '../../common/actor-avatar';
import { useViewingTimezone } from '../../common/use-viewing-timezone';
import { ProviderLogo } from './provider-logo';
import { HealthIcon, useHealthLabel } from './shared';
import { DeleteRuntimeDialog } from './delete-runtime-dialog';
import { DeleteRuntimeProfileDialog } from './delete-runtime-profile-dialog';
import { RuntimeProfilesDialog } from './runtime-profiles-dialog';
import { computeCostInWindow, pctChange } from '../utils';
import { runtimeRowLabel } from './runtime-machines';
import { isPendingCustomRuntimeWarning } from './pending-runtime';
import { useT, useTimeAgo } from '../../i18n';

function isPlaceholderRow(availability: RuntimeAvailability): boolean {
  return availability === 'registering' || availability === 'disabled';
}

const GRID_COLS =
  'grid-cols-[0.75rem_minmax(120px,1fr)_var(--rtc-health)_var(--rtc-kebab)_0.75rem] ' +
  '@2xl:grid-cols-[0.75rem_minmax(140px,1fr)_var(--rtc-health)_var(--rtc-owner)_var(--rtc-agents)_var(--rtc-cost)_var(--rtc-cli)_var(--rtc-kebab)_0.75rem]';

const COLUMN_WIDTHS = {
  health: 176,
  owner: 96,
  agents: 92,
  cost: 96,
  cli: 112,
} as const;

const FIXED_TRACKS_WIDTH = 164 + 8 * 12;

function columnTrackVars(showOwner: boolean, showActions: boolean): React.CSSProperties {
  const minWidth =
    FIXED_TRACKS_WIDTH +
    COLUMN_WIDTHS.health +
    (showOwner ? COLUMN_WIDTHS.owner : 0) +
    COLUMN_WIDTHS.agents +
    COLUMN_WIDTHS.cost +
    COLUMN_WIDTHS.cli +
    (showActions ? 28 : 0);
  return {
    '--rtc-health': `${COLUMN_WIDTHS.health}px`,
    '--rtc-owner': showOwner ? `${COLUMN_WIDTHS.owner}px` : '0px',
    '--rtc-agents': `${COLUMN_WIDTHS.agents}px`,
    '--rtc-cost': `${COLUMN_WIDTHS.cost}px`,
    '--rtc-cli': `${COLUMN_WIDTHS.cli}px`,
    '--rtc-kebab': showActions ? '1.75rem' : '0px',
    '--rtc-minw': `${minWidth}px`,
  } as React.CSSProperties;
}

interface RuntimeWorkload {
  agentIds: string[];
  runningCount: number;
  queuedCount: number;
}

const EMPTY_WORKLOAD: RuntimeWorkload = {
  agentIds: [],
  runningCount: 0,
  queuedCount: 0,
};

export interface RuntimeRow {
  runtime: AgentRuntime;
  profile: RuntimeProfile | null;
  ownerMember: MemberWithUser | null;
  workload: RuntimeWorkload;
  canDelete: boolean;
}

export function buildWorkloadIndex(
  agents: Agent[],
  tasks: AgentTask[],
): Map<string, RuntimeWorkload> {
  const result = new Map<string, RuntimeWorkload>();
  const agentToRuntime = new Map<string, string>();

  for (const a of agents) {
    if (!a.runtime_id || a.archived_at) continue;
    agentToRuntime.set(a.id, a.runtime_id);
    const entry = result.get(a.runtime_id) ?? {
      agentIds: [],
      runningCount: 0,
      queuedCount: 0,
    };
    entry.agentIds.push(a.id);
    result.set(a.runtime_id, entry);
  }
  for (const t of tasks) {
    const rid = agentToRuntime.get(t.agent_id);
    if (!rid) continue;
    const entry = result.get(rid);
    if (!entry) continue;
    if (t.status === 'running') entry.runningCount += 1;
    else if (t.status === 'queued' || t.status === 'dispatched') entry.queuedCount += 1;
  }
  return result;
}

function RuntimeNameCell({
  runtime,
  availability,
  machineTitle,
}: {
  runtime: AgentRuntime;
  availability: RuntimeAvailability;
  machineTitle?: string;
}) {
  const label = runtimeRowLabel(runtime, machineTitle ?? '');
  return (
    <ListGridCell className="gap-2">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center">
        <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      </div>
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="block min-w-0 shrink truncate text-sm font-medium">{label}</span>
        <RuntimeKindBadge runtime={runtime} />
        <PendingRuntimeBadge availability={availability} />
        <VisibilityBadge runtime={runtime} />
      </div>
    </ListGridCell>
  );
}

function RuntimeKindBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT('runtimes');
  const isCustom = !!runtime.profile_id;
  return (
    <span
      className={
        isCustom
          ? 'inline-flex shrink-0 items-center rounded bg-info/10 px-1 text-[10px] font-medium text-info'
          : 'inline-flex shrink-0 items-center rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground'
      }
    >
      {isCustom ? t(($) => $.list.badge_custom) : t(($) => $.list.badge_builtin)}
    </span>
  );
}

function PendingRuntimeBadge({ availability }: { availability: RuntimeAvailability }) {
  const { t } = useT('runtimes');
  if (availability === 'disabled') {
    return (
      <span className="inline-flex shrink-0 items-center rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground">
        {t(($) => $.list.badge_disabled)}
      </span>
    );
  }
  if (availability === 'registering') {
    return (
      <span className="inline-flex shrink-0 items-center rounded bg-warning/10 px-1 text-[10px] font-medium text-warning">
        {t(($) => $.list.badge_registering)}
      </span>
    );
  }
  return null;
}

function VisibilityBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT('runtimes');
  if (runtime.visibility !== 'public') return null;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-0.5 rounded bg-info/10 px-1 text-[10px] font-medium text-info">
            <Globe className="h-2.5 w-2.5" />
            {t(($) => $.detail.visibility_label.public)}
          </span>
        }
      />
      <TooltipContent>{t(($) => $.detail.visibility_hint.public)}</TooltipContent>
    </Tooltip>
  );
}

export function HealthCell({
  runtime,
  workload,
  now,
}: {
  runtime: AgentRuntime;
  workload: RuntimeWorkload;
  now: number;
}) {
  const { t } = useT('runtimes');
  const { t: tAgents } = useT('agents');
  const labelOf = useHealthLabel();
  const timeAgo = useTimeAgo();
  const state = deriveRuntimeState(runtime, now);

  if (state.availability !== 'live') {
    if (state.availability === 'disabled') {
      return (
        <ListGridCell>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.list.pending_health_disabled)}
          </span>
        </ListGridCell>
      );
    }
    if (state.availability === 'unavailable_here') {
      return (
        <ListGridCell className="gap-1.5">
          <MonitorOff className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span
            className="block min-w-0 truncate text-xs text-muted-foreground"
            title={state.failureReason ?? undefined}
          >
            {t(($) => $.list.health_unavailable_here)}
          </span>
        </ListGridCell>
      );
    }
    const warning = isPendingCustomRuntimeWarning(runtime, now);
    return (
      <ListGridCell className="gap-1.5">
        {warning ? (
          <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
        ) : (
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-info" />
        )}
        <span className="block min-w-0 truncate text-xs">
          {warning ? t(($) => $.list.pending_health_warning) : t(($) => $.list.pending_health)}
        </span>
      </ListGridCell>
    );
  }

  const { health } = state;
  const offline = health === 'offline' || health === 'about_to_gc';
  const lastSeen = state.lastSeenAt ? timeAgo(state.lastSeenAt) : null;
  const active = workload.runningCount + workload.queuedCount;

  return (
    <ListGridCell className="gap-1.5">
      <HealthIcon health={health} />
      <span className="block min-w-0 truncate text-xs">
        {labelOf(health)}
        {health !== 'online' && lastSeen && (
          <span className="text-muted-foreground"> · {lastSeen}</span>
        )}
        {!offline && active > 0 && (
          <span className="text-muted-foreground">
            {' · '}
            {tAgents(($) => $.row.task_count, { count: active })}
          </span>
        )}
      </span>
    </ListGridCell>
  );
}

const COST_CELL_DAYS = 14;

export function CostCell({ runtimeId }: { runtimeId: string }) {
  const { t, i18n } = useT('runtimes');
  const tz = useViewingTimezone();
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const { data: usage = [] } = useQuery(runtimeUsageOptions(runtimeId, COST_CELL_DAYS, tz));
  const cost7d = useMemo(() => computeCostInWindow(usage, 7, tz), [usage, tz]);
  const costPrev7d = useMemo(() => computeCostInWindow(usage, 7, tz, 7), [usage, tz]);
  const delta = pctChange(cost7d, costPrev7d);

  if (usage.length === 0) {
    return (
      <div className="w-full text-right">
        <span className="text-xs text-muted-foreground/50">—</span>
      </div>
    );
  }
  const fmt = cost7d >= 100 ? `$${cost7d.toFixed(0)}` : `$${cost7d.toFixed(2)}`;
  const deltaTone =
    delta == null
      ? 'text-muted-foreground'
      : delta > 0
        ? 'text-warning'
        : delta < 0
          ? 'text-success'
          : 'text-muted-foreground';
  const deltaLabel =
    delta == null
      ? null
      : delta === 0
        ? t(($) => $.list.cost_delta_flat)
        : `${delta > 0 ? '↑' : '↓'}${Math.abs(delta)}%`;
  return (
    <div className="flex w-full flex-col items-end leading-tight">
      <CurrencyNumberFlow
        value={cost7d}
        locales={locales}
        aria-label={fmt}
        className="text-sm font-medium"
      />
      {deltaLabel && <span className={`text-[11px] tabular-nums ${deltaTone}`}>{deltaLabel}</span>}
    </div>
  );
}

export function CliCell({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT('runtimes');
  const state = deriveRuntimeState(runtime, 0);
  if (state.availability !== 'live') {
    const { commandName } = state;
    if (!commandName) {
      return (
        <span className="text-xs text-muted-foreground/50">
          {t(($) => $.list.pending_cli_unknown)}
        </span>
      );
    }
    return (
      <div className="flex min-w-0 flex-col text-xs">
        <span
          className="truncate font-mono text-muted-foreground"
          title={state.failureReason ?? commandName}
        >
          {commandName}
        </span>
        {state.failureReason && (
          <span className="truncate text-muted-foreground/70" title={state.failureReason}>
            {state.failureReason}
          </span>
        )}
      </div>
    );
  }

  if (runtime.runtime_mode === 'cloud') {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const meta = runtime.metadata as Record<string, unknown> | null;
  const version = meta && typeof meta.version === 'string' ? meta.version : null;

  if (!version) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }

  return (
    <div className="flex min-w-0 items-center text-xs">
      <span className="truncate font-mono text-muted-foreground">{version}</span>
    </div>
  );
}

function AgentStack({ agentIds }: { agentIds: string[] }) {
  if (agentIds.length === 0) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const visible = agentIds.slice(0, 3);
  const extra = agentIds.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
      {visible.map((id) => (
        <span key={id} className="inline-flex rounded-full ring-2 ring-background">
          <ActorAvatar actorType="agent" actorId={id} size="md" enableHoverCard />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
          +{extra}
        </span>
      )}
    </div>
  );
}

export function RuntimeRowMenu({
  runtime,
  profile,
  wsId,
  canDelete,
}: {
  runtime: AgentRuntime;
  profile: RuntimeProfile | null;
  wsId: string;
  canDelete: boolean;
}) {
  const { t } = useT('runtimes');
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const isCustomRuntime = !!runtime.profile_id;

  if (!canDelete) {
    return <span aria-hidden />;
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.list.row_actions_aria)}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover/row:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100 data-popup-open:text-accent-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-40">
          {isCustomRuntime && profile && (
            <DropdownMenuItem onClick={() => setEditOpen(true)}>
              <Pencil aria-hidden="true" className="h-3.5 w-3.5" />
              {t(($) => $.list.edit_action)}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
            title={t(($) => $.list.delete_permission_hint)}
          >
            <Trash2 aria-hidden="true" className="h-3.5 w-3.5" />
            {isCustomRuntime
              ? t(($) => $.list.delete_profile_action)
              : t(($) => $.list.delete_action)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {isCustomRuntime && profile && editOpen && (
        <RuntimeProfilesDialog
          wsId={wsId}
          intent="edit"
          initialProfile={profile}
          onClose={() => setEditOpen(false)}
        />
      )}
      {isCustomRuntime && profile ? (
        <DeleteRuntimeProfileDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          profile={profile}
          wsId={wsId}
          onDeleted={() => setDeleteOpen(false)}
        />
      ) : (
        <DeleteRuntimeDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          runtime={runtime}
          wsId={wsId}
          onDeleted={() => {
            setDeleteOpen(false);
            toast.success(t(($) => $.detail.toast_deleted));
          }}
        />
      )}
    </>
  );
}

export function RuntimeList({
  runtimes,
  now,
  runtimeHref,
  machineTitle,
}: {
  runtimes: AgentRuntime[];
  now: number;
  runtimeHref?: (runtimeId: string) => string;
  machineTitle?: string;
}) {
  const { t } = useT('runtimes');
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const rowLink = useRowLink();
  const user = useAuthStore((s) => s.user);

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: profiles = [] } = useQuery(runtimeProfileListOptions(wsId));

  const currentMember = user ? members.find((m) => m.user_id === user.id) : null;
  const isAdmin = currentMember
    ? currentMember.role === 'owner' || currentMember.role === 'admin'
    : false;

  const workloadIndex = useMemo(() => buildWorkloadIndex(agents, snapshot), [agents, snapshot]);

  const memberById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  const profileById = useMemo(() => {
    const map = new Map<string, RuntimeProfile>();
    for (const p of profiles) map.set(p.id, p);
    return map;
  }, [profiles]);

  const showOwner = useMemo(() => {
    const owners = new Set<string>();
    for (const r of runtimes) {
      if (r.owner_id) owners.add(r.owner_id);
    }
    return owners.size > 1;
  }, [runtimes]);

  const rows = useMemo<RuntimeRow[]>(() => {
    return runtimes.map((runtime) => {
      const profile = runtime.profile_id ? (profileById.get(runtime.profile_id) ?? null) : null;
      const isCustomRuntime = !!runtime.profile_id;
      const placeholder = isPlaceholderRow(deriveRuntimeState(runtime, now).availability);
      return {
        runtime,
        profile,
        ownerMember: runtime.owner_id ? (memberById.get(runtime.owner_id) ?? null) : null,
        workload: workloadIndex.get(runtime.id) ?? EMPTY_WORKLOAD,
        canDelete: isCustomRuntime
          ? isAdmin && !!profile
          : !placeholder && (isAdmin || (!!user && runtime.owner_id === user.id)),
      };
    });
  }, [runtimes, now, profileById, memberById, workloadIndex, isAdmin, user]);

  const showActions = rows.some((row) => row.canDelete);

  return (
    <div className="overflow-x-auto overflow-y-hidden @container">
      <ListGrid
        className={`${GRID_COLS} @2xl:min-w-[var(--rtc-minw)]`}
        style={columnTrackVars(showOwner, showActions)}
      >
        <ListGridHeader>
          <ListGridHeaderCell>{t(($) => $.list.col_runtime)}</ListGridHeaderCell>
          <ListGridHeaderCell>{t(($) => $.list.col_health)}</ListGridHeaderCell>
          {showOwner ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_owner)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          <ListGridHeaderCell className="hidden @2xl:flex">
            {t(($) => $.list.col_agents)}
          </ListGridHeaderCell>
          <ListGridHeaderCell className="hidden @2xl:flex" align="right">
            {t(($) => $.list.col_cost)}
          </ListGridHeaderCell>
          <ListGridHeaderCell className="hidden @2xl:flex">
            {t(($) => $.list.col_cli)}
          </ListGridHeaderCell>
          <span aria-hidden="true" />
        </ListGridHeader>
        {rows.map((row) => {
          const availability = deriveRuntimeState(row.runtime, now).availability;
          const placeholder = isPlaceholderRow(availability);
          return (
            <ListGridRow
              key={row.runtime.id}
              className={placeholder ? 'cursor-default' : 'cursor-pointer'}
              {...(!placeholder
                ? rowLink(runtimeHref?.(row.runtime.id) ?? wsPaths.runtimeDetail(row.runtime.id))
                : {})}
            >
              <RuntimeNameCell
                runtime={row.runtime}
                availability={availability}
                machineTitle={machineTitle}
              />
              <HealthCell runtime={row.runtime} workload={row.workload} now={now} />
              {showOwner ? (
                <ListGridCell className="hidden gap-1.5 @2xl:flex">
                  {row.ownerMember ? (
                    <>
                      <ActorAvatar actorType="member" actorId={row.ownerMember.user_id} size="sm" />
                      <span className="min-w-0 truncate text-xs text-muted-foreground">
                        {row.ownerMember.name}
                      </span>
                    </>
                  ) : (
                    <span className="text-xs text-muted-foreground/50">—</span>
                  )}
                </ListGridCell>
              ) : (
                <ListGridCell className="hidden px-0 @2xl:flex" />
              )}
              <ListGridCell className="hidden @2xl:flex">
                <AgentStack agentIds={row.workload.agentIds} />
              </ListGridCell>
              <ListGridCell className="hidden @2xl:flex">
                {placeholder ? (
                  <div className="w-full text-right">
                    <span className="text-xs text-muted-foreground/50">—</span>
                  </div>
                ) : (
                  <CostCell runtimeId={row.runtime.id} />
                )}
              </ListGridCell>
              <ListGridCell className="hidden @2xl:flex">
                <CliCell runtime={row.runtime} />
              </ListGridCell>
              <ListGridCell className="justify-end px-0">
                <span onClick={(e) => e.stopPropagation()} className="flex items-center">
                  <RuntimeRowMenu
                    runtime={row.runtime}
                    profile={row.profile}
                    wsId={wsId}
                    canDelete={row.canDelete}
                  />
                </span>
              </ListGridCell>
            </ListGridRow>
          );
        })}
      </ListGrid>
    </div>
  );
}
