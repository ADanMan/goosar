'use client';

import { useEffect, useMemo, useState } from 'react';
import { Loader2, Lock, Pencil, Plus, RefreshCw, Server, Trash2 } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { ApiError } from '@goosar/core/api';
import { runtimeCapabilitiesOptions, runtimeDisplayLabel } from '@goosar/core/runtimes';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@goosar/ui/components/ui/alert-dialog';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { toast } from 'sonner';
import { useT } from '../../../i18n';
import {
  listManagedMcpServers,
  removeManagedMcpServer,
  upsertManagedMcpServer,
  validateMcpServerConfig,
  type ManagedMcpServer,
} from './mcp-config-model';
import { McpServerDialog } from './mcp-server-dialog';

export function McpConfigTab({
  agent,
  runtime,
  canEdit,
  isOwner,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  isOwner: boolean;
  canEdit: boolean;
  onSave: (updates: { mcp_config: unknown | null }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT('agents');
  const runtimeId =
    runtime?.runtime_mode === 'local' && runtime.status === 'online' ? runtime.id : null;
  const runtimeQuery = useQuery(runtimeCapabilitiesOptions(runtimeId));
  const redacted = agent.mcp_config_redacted === true;
  const managedServers = useMemo(() => listManagedMcpServers(agent.mcp_config), [agent.mcp_config]);
  const hasMaskedServers = managedServers.some((server) => server.masked);
  const readable = !redacted || managedServers.length > 0;
  const manageable = canEdit && readable;
  const canAuthorServers = manageable && isOwner;
  const managedNames = useMemo(
    () => new Set(managedServers.map((server) => server.name)),
    [managedServers],
  );
  const managedRows = useMemo(
    () =>
      managedServers.map((server) => {
        if (server.masked) return { ...server, issue: undefined };
        const { errors, warnings } = validateMcpServerConfig(server.config);
        return {
          ...server,
          issue:
            errors.length > 0
              ? ('invalid' as const)
              : warnings.includes('command_registry_runner')
                ? ('registry' as const)
                : undefined,
        };
      }),
    [managedServers],
  );
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<ManagedMcpServer | null>(null);
  const [deletingServer, setDeletingServer] = useState<ManagedMcpServer | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => onDirtyChange?.(false), [onDirtyChange]);

  const openAddDialog = () => {
    setEditingServer(null);
    setEditorOpen(true);
  };

  const openEditDialog = (server: ManagedMcpServer) => {
    setEditingServer(server);
    setEditorOpen(true);
  };

  const handleSaveServer = async (name: string, config: Record<string, unknown>) => {
    const next = upsertManagedMcpServer(agent.mcp_config, editingServer, name, config);
    try {
      await onSave({ mcp_config: next });
      toast.success(
        editingServer
          ? t(($) => $.tab_body.mcp_config.updated_toast)
          : t(($) => $.tab_body.mcp_config.added_toast),
      );
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.mcp_config.save_failed_toast),
      );
      throw error;
    }
  };

  const handleDelete = async () => {
    if (!deletingServer) return;
    setDeleting(true);
    try {
      await onSave({
        mcp_config: removeManagedMcpServer(agent.mcp_config, deletingServer),
      });
      toast.success(t(($) => $.tab_body.mcp_config.deleted_toast));
      setDeletingServer(null);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.mcp_config.delete_failed_toast),
      );
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="space-y-8">
      <p className="max-w-2xl break-words text-pretty text-sm leading-6 text-muted-foreground">
        {t(($) => $.tab_body.mcp_config.intro)}
      </p>

      <section className="space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h3 className="text-sm font-medium">{t(($) => $.tab_body.mcp_config.managed_title)}</h3>
            <p className="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">
              {t(($) => $.tab_body.mcp_config.managed_hint)}
            </p>
          </div>
          {canAuthorServers && (
            <Button size="sm" variant="outline" onClick={openAddDialog}>
              <Plus aria-hidden="true" />
              {t(($) => $.tab_body.mcp_config.add_action)}
            </Button>
          )}
        </div>

        {hasMaskedServers && manageable && (
          <p
            data-testid="mcp-masked-notice"
            className="flex items-start gap-2 rounded-md border border-border bg-muted/40 p-2.5 text-xs text-muted-foreground"
          >
            <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            <span>{t(($) => $.tab_body.mcp_config.masked_notice)}</span>
          </p>
        )}

        {!readable ? (
          <div className="flex items-start gap-2 rounded-lg border px-4 py-3">
            <Lock className="mt-0.5 h-4 w-4 text-muted-foreground" aria-hidden="true" />
            <div>
              <p className="text-sm font-medium">
                {t(($) => $.tab_body.mcp_config.redacted_title)}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.tab_body.mcp_config.redacted_hint)}
              </p>
            </div>
          </div>
        ) : managedServers.length > 0 ? (
          <McpServerList
            servers={managedRows}
            disabledLabel={t(($) => $.tab_body.mcp_config.agent_disabled_badge)}
            invalidLabel={t(($) => $.tab_body.mcp_config.invalid_badge)}
            registryLabel={t(($) => $.tab_body.mcp_config.registry_badge)}
            maskedLabel={t(($) => $.tab_body.mcp_config.masked_badge)}
            onEdit={canAuthorServers ? openEditDialog : undefined}
            onDelete={manageable ? setDeletingServer : undefined}
            editLabel={t(($) => $.tab_body.mcp_config.edit_aria)}
            deleteLabel={t(($) => $.tab_body.mcp_config.delete_aria)}
          />
        ) : (
          <McpNotice text={t(($) => $.tab_body.mcp_config.managed_empty)} />
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h3 className="text-sm font-medium">{t(($) => $.tab_body.mcp_config.runtime_title)}</h3>
            <p className="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">
              {t(($) => $.tab_body.mcp_config.runtime_hint, {
                runtime: runtime ? runtimeDisplayLabel(runtime) : 'Runtime',
              })}
            </p>
          </div>
          {runtimeId && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => runtimeQuery.refetch()}
              disabled={runtimeQuery.isFetching}
            >
              <RefreshCw
                className={
                  runtimeQuery.isFetching
                    ? 'h-3.5 w-3.5 animate-spin motion-reduce:animate-none'
                    : 'h-3.5 w-3.5'
                }
                aria-hidden="true"
              />
              {t(($) => $.tab_body.mcp_config.refresh_action)}
            </Button>
          )}
        </div>
        {!runtime ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_missing)} />
        ) : runtime.status !== 'online' ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_offline)} />
        ) : runtimeQuery.isLoading ? (
          <McpNotice loading text={t(($) => $.tab_body.mcp_config.runtime_discovering)} />
        ) : runtimeQuery.isError ? (
          <McpNotice
            text={
              runtimeQuery.error instanceof ApiError && runtimeQuery.error.status === 403
                ? t(($) => $.tab_body.mcp_config.runtime_forbidden)
                : t(($) => $.tab_body.mcp_config.runtime_failed)
            }
          />
        ) : runtimeQuery.data?.mcpSupported !== true ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_unsupported)} />
        ) : runtimeQuery.data.mcpServers.length === 0 ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_empty)} />
        ) : (
          <McpServerList
            servers={runtimeQuery.data.mcpServers.map((server) => ({
              name: server.name,
              transport: server.transport || 'unknown',
              enabled: server.enabled,
              source: server.source,
              overridden: managedNames.has(server.name),
            }))}
            disabledLabel={t(($) => $.tab_body.mcp_config.runtime_disabled_badge)}
            overriddenLabel={t(($) => $.tab_body.mcp_config.runtime_overridden_badge)}
          />
        )}
      </section>

      {manageable && (
        <McpServerDialog
          open={editorOpen}
          server={editingServer}
          existingNames={managedNames}
          onOpenChange={setEditorOpen}
          onSave={handleSaveServer}
        />
      )}

      <AlertDialog
        open={deletingServer !== null}
        onOpenChange={(open) => !open && !deleting && setDeletingServer(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.tab_body.mcp_config.delete_dialog_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.tab_body.mcp_config.delete_dialog_description, {
                name: deletingServer?.name ?? '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.tab_body.mcp_config.dialog_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting && (
                <Loader2
                  className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                  aria-hidden="true"
                />
              )}
              {t(($) => $.tab_body.mcp_config.delete_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

type McpServerView = {
  name: string;
  container?: string;
  transport: string;
  enabled: boolean;
  source?: string;
  overridden?: boolean;
  issue?: 'invalid' | 'registry';
  masked?: boolean;
};

function McpServerList({
  servers,
  disabledLabel,
  overriddenLabel,
  invalidLabel,
  registryLabel,
  maskedLabel,
  onEdit,
  onDelete,
  editLabel,
  deleteLabel,
}: {
  servers: McpServerView[];
  disabledLabel: string;
  overriddenLabel?: string;
  invalidLabel?: string;
  registryLabel?: string;
  maskedLabel?: string;
  onEdit?: (server: ManagedMcpServer) => void;
  onDelete?: (server: ManagedMcpServer) => void;
  editLabel?: string;
  deleteLabel?: string;
}) {
  return (
    <ul className="divide-y rounded-lg border bg-surface-raised/40">
      {servers.map((server) => (
        <li
          key={`${server.container ?? ''}:${server.name}`}
          className="flex items-center gap-3 p-3"
        >
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
            <Server className="h-4 w-4" aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{server.name}</p>
            {server.transport || server.source ? (
              <p className="text-xs text-muted-foreground">
                <span className="uppercase">{server.transport}</span>
                {server.source ? ` · ${server.source}` : null}
              </p>
            ) : null}
          </div>
          {server.masked && maskedLabel ? <Badge variant="outline">{maskedLabel}</Badge> : null}
          {server.issue === 'invalid' && invalidLabel ? (
            <Badge variant="destructive">{invalidLabel}</Badge>
          ) : server.issue === 'registry' && registryLabel ? (
            <Badge variant="outline" className="border-warning/40 text-warning">
              {registryLabel}
            </Badge>
          ) : null}
          {server.overridden && overriddenLabel ? (
            <Badge variant="outline">{overriddenLabel}</Badge>
          ) : !server.enabled ? (
            <Badge variant="outline">{disabledLabel}</Badge>
          ) : null}
          {/*
            Edit and delete are granted independently, so each button checks
            its own callback. They used to share one `onEdit && onDelete`
            guard, which quietly coupled them: withholding edit from a viewer
            who may still delete took the delete button away with it.

            No edit affordance for a masked row either: the editor opens on
            the current configuration, and for this viewer that is a
            placeholder. Saving it would write `{"__goosar_masked__": true}`
            shaped input the server rejects — or, worse, look like a
            successful edit of a config they never saw. Delete stays
            available and is honest about what it does.
          */}
          {(onEdit || onDelete) && (
            <div className="flex items-center gap-1">
              {onEdit && !server.masked && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${editLabel} ${server.name}`}
                  onClick={() => onEdit(server as ManagedMcpServer)}
                >
                  <Pencil aria-hidden="true" />
                </Button>
              )}
              {onDelete && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${deleteLabel} ${server.name}`}
                  onClick={() => onDelete(server as ManagedMcpServer)}
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              )}
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

function McpNotice({ text, loading = false }: { text: string; loading?: boolean }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-6 text-xs text-muted-foreground">
      {loading ? (
        <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />
      ) : (
        <Server className="h-4 w-4" aria-hidden="true" />
      )}
      {text}
    </div>
  );
}
