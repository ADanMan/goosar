'use client';

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Search, ServerOff } from 'lucide-react';
import { Alert, AlertDescription } from '@goosar/ui/components/ui/alert';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@goosar/ui/components/ui/empty';
import { Input } from '@goosar/ui/components/ui/input';
import type { DeploymentFleetMachine } from '@goosar/core/api/deployment-fleet';
import { deploymentFleetOptions } from '@goosar/core/deployment/admin';
import { useT, useUiLocale } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';

type FleetFilter = 'all' | 'online' | 'offline' | 'outdated';

const AGE_TICK_MS = 1_000;

export function DeploymentFleetSection() {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const [filter, setFilter] = useState<FleetFilter>('all');
  const [search, setSearch] = useState('');
  const { data: fleet, isError, isPending, dataUpdatedAt } = useQuery(deploymentFleetOptions());

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), AGE_TICK_MS);
    return () => clearInterval(id);
  }, []);

  const machines = useMemo(() => {
    const all = fleet?.machines ?? [];
    const byFilter =
      filter === 'online'
        ? all.filter((m) => m.online === true)
        : filter === 'offline'
          ? all.filter((m) => m.online !== true)
          : filter === 'outdated'
            ? all.filter((m) => m.version_outdated === true)
            : all;
    const query = search.trim().toLowerCase();
    if (query === '') return byFilter;
    return byFilter.filter((m) =>
      [m.device_info, m.daemon_id, m.workspace_name, m.owner_email]
        .join(' ')
        .toLowerCase()
        .includes(query),
    );
  }, [fleet, filter, search]);

  const summary = fleet?.summary;
  const ageSeconds = dataUpdatedAt > 0 ? Math.max(0, Math.round((now - dataUpdatedAt) / 1000)) : 0;

  const formatHeartbeat = (raw: string | null): string => {
    if (raw === null || raw === '') return t(($) => $.deployment.fleet.never);
    const at = new Date(raw);
    if (Number.isNaN(at.getTime())) return raw;
    const seconds = Math.max(0, Math.round((now - at.getTime()) / 1000));
    if (seconds < 60) return t(($) => $.deployment.fleet.seconds_ago, { seconds });
    if (seconds < 3600)
      return t(($) => $.deployment.fleet.minutes_ago, {
        minutes: Math.floor(seconds / 60),
      });
    return at.toLocaleString(uiLocale);
  };

  return (
    <SettingsSection
      title={t(($) => $.deployment.fleet.title)}
      description={t(($) => $.deployment.fleet.description)}
      action={
        <span className="text-xs whitespace-nowrap text-muted-foreground">
          {t(($) => $.deployment.fleet.updated_ago, { seconds: ageSeconds })}
        </span>
      }
    >
      {summary !== undefined ? (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <FleetTile
            label={t(($) => $.deployment.fleet.tile_online)}
            value={summary.machines_online}
          />
          <FleetTile
            label={t(($) => $.deployment.fleet.tile_offline)}
            value={summary.machines_offline}
            alarming={summary.machines_offline > 0}
          />
          <FleetTile
            label={t(($) => $.deployment.fleet.tile_outdated)}
            value={summary.outdated_versions}
            alarming={summary.outdated_versions > 0}
          />
          <FleetTile
            label={t(($) => $.deployment.fleet.tile_stuck)}
            value={summary.stuck_tasks}
            alarming={summary.stuck_tasks > 0}
          />
        </div>
      ) : null}

      <div className="relative sm:w-72">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.deployment.fleet.search_placeholder)}
          aria-label={t(($) => $.deployment.fleet.search_placeholder)}
          className="pl-8"
        />
      </div>

      <div className="flex flex-wrap gap-1.5">
        {(['all', 'online', 'offline', 'outdated'] as const).map((value) => (
          <Button
            key={value}
            type="button"
            size="sm"
            variant={filter === value ? 'secondary' : 'ghost'}
            aria-pressed={filter === value}
            onClick={() => setFilter(value)}
          >
            {value === 'all'
              ? t(($) => $.deployment.fleet.filter_all)
              : value === 'online'
                ? t(($) => $.deployment.fleet.filter_online)
                : value === 'offline'
                  ? t(($) => $.deployment.fleet.filter_offline)
                  : t(($) => $.deployment.fleet.filter_outdated)}
          </Button>
        ))}
      </div>

      {isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t(($) => $.deployment.fleet.failed)}</AlertDescription>
        </Alert>
      ) : !isPending && machines.length === 0 ? (
        <SettingsCard>
          <Empty className="border-none py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ServerOff />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.deployment.fleet.empty_title)}</EmptyTitle>
              <EmptyDescription>
                {(fleet?.machines.length ?? 0) === 0
                  ? t(($) => $.deployment.fleet.empty)
                  : t(($) => $.deployment.fleet.no_matches)}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </SettingsCard>
      ) : (
        <SettingsCard>
          {isPending ? (
            <div className="px-4 py-3.5 text-sm text-muted-foreground">
              {t(($) => $.deployment.fleet.loading)}
            </div>
          ) : (
            machines.map((machine, index) => (
              <FleetRow
                key={`${machine.daemon_id}:${machine.workspace_id}:${index}`}
                machine={machine}
                heartbeat={formatHeartbeat(machine.last_heartbeat_at)}
              />
            ))
          )}
        </SettingsCard>
      )}

      {fleet?.truncated === true ? (
        <p className="px-0.5 text-xs text-muted-foreground">
          {t(($) => $.deployment.fleet.truncated, {
            shown: machines.length,
            total: fleet.total,
          })}
        </p>
      ) : null}
    </SettingsSection>
  );
}

function FleetTile({
  label,
  value,
  alarming = false,
}: {
  label: string;
  value: number;
  alarming?: boolean;
}) {
  return (
    <div className="rounded-md border border-surface-border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={alarming ? 'text-lg font-semibold text-destructive' : 'text-lg font-semibold'}
      >
        {value}
      </div>
    </div>
  );
}

function FleetRow({ machine, heartbeat }: { machine: DeploymentFleetMachine; heartbeat: string }) {
  const { t } = useT('settings');
  const name =
    machine.device_info !== ''
      ? machine.device_info
      : machine.daemon_id !== ''
        ? machine.daemon_id
        : t(($) => $.deployment.fleet.unnamed_machine);

  return (
    <div className="space-y-1.5 px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span
          aria-hidden="true"
          className={
            machine.online === true
              ? 'size-2 shrink-0 rounded-full bg-emerald-500'
              : 'size-2 shrink-0 rounded-full bg-muted-foreground'
          }
        />
        <span className="text-sm font-medium break-all">{name}</span>
        <Badge variant="outline" className="font-normal">
          {machine.workspace_name}
        </Badge>
        {machine.version_outdated === true ? (
          <Badge variant="destructive">
            {t(($) => $.deployment.fleet.version_outdated, {
              version: machine.client_version,
            })}
          </Badge>
        ) : machine.client_version !== '' ? (
          <Badge variant="outline" className="font-normal">
            {machine.client_version}
          </Badge>
        ) : null}
        {machine.stuck_tasks > 0 ? (
          <Badge variant="destructive">
            {t(($) => $.deployment.fleet.stuck_badge, {
              count: machine.stuck_tasks,
            })}
          </Badge>
        ) : null}
      </div>
      <div className="text-xs break-all text-muted-foreground">
        {t(($) => $.deployment.fleet.row_meta, {
          owner:
            machine.owner_email !== ''
              ? machine.owner_email
              : t(($) => $.deployment.fleet.owner_unknown),
          heartbeat,
          running: machine.running_tasks,
        })}
      </div>
      {machine.runtimes.map((runtime, index) => (
        <div key={`${runtime.id}:${index}`} className="text-xs break-all text-muted-foreground">
          {t(($) => $.deployment.fleet.runtime_line, {
            name: runtime.name,
            visibility:
              runtime.visibility === 'private'
                ? t(($) => $.deployment.fleet.visibility_private)
                : runtime.visibility === 'public'
                  ? t(($) => $.deployment.fleet.visibility_public)
                  : runtime.visibility,
            status:
              runtime.status === 'online'
                ? t(($) => $.deployment.fleet.status_online)
                : runtime.status === 'offline'
                  ? t(($) => $.deployment.fleet.status_offline)
                  : runtime.status,
            agents:
              runtime.agents.length === 0
                ? t(($) => $.deployment.fleet.no_agents)
                : runtime.agents
                    .map((a) => (a.system_key !== '' ? `${a.name} (${a.system_key})` : a.name))
                    .join(', '),
          })}
        </div>
      ))}
    </div>
  );
}
