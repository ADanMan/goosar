'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Loader2, Server, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import type { WorkspaceMcpServer } from '@goosar/core/api/workspace-mcp';
import {
  agentMcpServersOptions,
  useAddAgentMcpServer,
  useRemoveAgentMcpServer,
  useSetAgentMcpServerEnabled,
  workspaceMcpServersOptions,
} from '@goosar/core/workspace/mcp-servers';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@goosar/ui/components/ui/select';
import { Switch } from '@goosar/ui/components/ui/switch';
import { useT } from '../../../i18n';

export function SharedMcpServersSection({
  wsId,
  agentId,
  canEdit,
}: {
  wsId: string;
  agentId: string;
  canEdit: boolean;
}) {
  const { t } = useT('agents');
  const assignedQuery = useQuery(agentMcpServersOptions(wsId, agentId));
  const libraryQuery = useQuery({
    ...workspaceMcpServersOptions(wsId),
    enabled: canEdit && wsId !== '',
  });
  const addServer = useAddAgentMcpServer(wsId, agentId);
  const setEnabled = useSetAgentMcpServerEnabled(wsId, agentId);
  const removeServer = useRemoveAgentMcpServer(wsId, agentId);

  const [picked, setPicked] = useState('');

  const assignedData = assignedQuery.data;
  const assigned = useMemo(() => assignedData ?? [], [assignedData]);
  const assignable = useMemo(() => {
    const taken = new Set(assigned.map((server) => server.id));
    return (libraryQuery.data ?? [])
      .filter((server) => !taken.has(server.id))
      .map((server) => ({ value: server.id, label: server.name }));
  }, [assigned, libraryQuery.data]);

  const notify = (error: unknown, fallback: string) => {
    toast.error(error instanceof Error && error.message ? error.message : fallback);
  };

  const handleAdd = async () => {
    if (picked === '') return;
    try {
      await addServer.mutateAsync(picked);
      setPicked('');
    } catch (error) {
      notify(
        error,
        t(($) => $.shared_mcp.add_failed_toast),
      );
    }
  };

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-sm font-medium">{t(($) => $.shared_mcp.title)}</h3>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {t(($) => $.shared_mcp.description)}
          </p>
        </div>
      </div>

      {assignedQuery.isLoading ? (
        <div className="flex items-center justify-center py-6 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
        </div>
      ) : assigned.length === 0 ? (
        <div className="rounded-md border border-border px-4 py-6 text-center">
          <Server className="mx-auto h-5 w-5 text-muted-foreground" />
          <p className="mt-2 text-xs text-muted-foreground">{t(($) => $.shared_mcp.empty)}</p>
        </div>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {assigned.map((server) => (
            <AssignedRow
              key={server.id}
              server={server}
              canEdit={canEdit}
              busy={setEnabled.isPending || removeServer.isPending}
              onToggle={async (enabled) => {
                try {
                  await setEnabled.mutateAsync({
                    serverId: server.id,
                    enabled,
                  });
                } catch (error) {
                  notify(
                    error,
                    t(($) => $.shared_mcp.toggle_failed_toast),
                  );
                }
              }}
              onRemove={async () => {
                try {
                  await removeServer.mutateAsync(server.id);
                } catch (error) {
                  notify(
                    error,
                    t(($) => $.shared_mcp.remove_failed_toast),
                  );
                }
              }}
            />
          ))}
        </ul>
      )}

      {canEdit ? (
        <p className="text-xs text-muted-foreground">{t(($) => $.shared_mcp.assign_warning)}</p>
      ) : null}

      {canEdit ? (
        <div className="flex items-center gap-2">
          <Select
            items={assignable}
            value={picked}
            onValueChange={(value) => setPicked(value ?? '')}
          >
            <SelectTrigger
              size="sm"
              className="w-full max-w-xs"
              aria-label={t(($) => $.shared_mcp.assign_placeholder)}
            >
              <SelectValue placeholder={t(($) => $.shared_mcp.assign_placeholder)}>
                {assignable.find((option) => option.value === picked)?.label}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {assignable.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            size="sm"
            disabled={picked === '' || addServer.isPending}
            onClick={() => void handleAdd()}
          >
            {addServer.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t(($) => $.shared_mcp.assign)}
          </Button>
        </div>
      ) : null}

      {canEdit && assignable.length === 0 && (libraryQuery.data ?? []).length === 0 ? (
        <p className="text-xs text-muted-foreground">{t(($) => $.shared_mcp.library_empty)}</p>
      ) : null}
    </section>
  );
}

function AssignedRow({
  server,
  canEdit,
  busy,
  onToggle,
  onRemove,
}: {
  server: WorkspaceMcpServer;
  canEdit: boolean;
  busy: boolean;
  onToggle: (enabled: boolean) => Promise<void>;
  onRemove: () => Promise<void>;
}) {
  const { t } = useT('agents');
  const enabled = server.enabled === true;
  const missing = server.missing_credentials ?? [];
  const schema = server.credential_schema ?? [];
  const missingFields = schema.filter((field) => missing.includes(field.key));
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{server.name}</span>
        <p className="mt-0.5 text-xs text-muted-foreground">{server.transport || 'unknown'}</p>
        {/*
          A5 (#347), said where somebody meets the broken integration rather
          than in a log. The markers here are the AGENT OWNER's, because the
          owner is whose credentials this server runs with — so this is a fact
          about whether the agent will work, not about whoever is reading.
          Missing means the server is dropped from the agent's next task; the
          row says so, names the fields, and repeats the admin's own
          where-to-find-it text.
        */}
        {missing.length > 0 ? (
          <div className="mt-1 space-y-0.5">
            <p className="text-xs leading-5 text-warning">
              {t(($) => $.shared_mcp.credentials_missing, {
                name: server.name,
                fields: missing.join(', '),
              })}
            </p>
            {missingFields.map((field) =>
              field.hint && field.hint !== '' ? (
                <p key={field.key} className="text-xs leading-5 text-muted-foreground">
                  {field.label && field.label !== '' ? `${field.label} (${field.key})` : field.key}
                  {' — '}
                  {field.hint}
                </p>
              ) : null,
            )}
            <p className="text-xs leading-5 text-muted-foreground">
              {t(($) => $.shared_mcp.credentials_where)}
            </p>
          </div>
        ) : null}
      </div>
      <Switch
        checked={enabled}
        disabled={!canEdit || busy}
        aria-label={t(($) => $.shared_mcp.toggle_label, { name: server.name })}
        onCheckedChange={(next) => void onToggle(next === true)}
      />
      {canEdit ? (
        <Button
          variant="ghost"
          size="icon"
          disabled={busy}
          aria-label={t(($) => $.shared_mcp.remove_label, {
            name: server.name,
          })}
          onClick={() => void onRemove()}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      ) : null}
    </li>
  );
}
