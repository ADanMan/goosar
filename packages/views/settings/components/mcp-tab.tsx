'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { KeyRound, Loader2, Pencil, Plus, Server, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import type { DeploymentMcpServer } from '@goosar/core/api/deployment-mcp';
import type { WorkspaceMcpServer } from '@goosar/core/api/workspace-mcp';
import {
  useSetWorkspaceDeploymentMcpServerEnabled,
  workspaceDeploymentMcpServersOptions,
} from '@goosar/core/deployment/mcp-servers';
import { useCurrentMember } from '@goosar/core/permissions';
import {
  useCreateWorkspaceMcpServer,
  useDeleteWorkspaceMcpServer,
  useSetWorkspaceMcpCredentials,
  useUpdateWorkspaceMcpServer,
  workspaceMcpServersOptions,
} from '@goosar/core/workspace/mcp-servers';
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
import { Switch } from '@goosar/ui/components/ui/switch';
import type { McpCredentialField } from '@goosar/core/api/workspace-mcp';
import { McpServerDialog } from '../../agents/components/tabs/mcp-server-dialog';
import { McpCredentialSchemaDialog } from './mcp-credential-schema-dialog';
import { McpCredentialsDialog } from './mcp-credentials-dialog';
import type { ManagedMcpServer } from '../../agents/components/tabs/mcp-config-model';
import { useT } from '../../i18n';
import { SettingsCard, SettingsSection, SettingsTab } from './settings-layout';

export function McpTab({ wsId }: { wsId: string }) {
  const { t } = useT('settings');
  const currentMember = useCurrentMember(wsId);
  const canManage = currentMember.role === 'owner' || currentMember.role === 'admin';

  const serversQuery = useQuery(workspaceMcpServersOptions(wsId));
  const createServer = useCreateWorkspaceMcpServer(wsId);
  const updateServer = useUpdateWorkspaceMcpServer(wsId);
  const deleteServer = useDeleteWorkspaceMcpServer(wsId);
  const setCredentials = useSetWorkspaceMcpCredentials(wsId);

  const serversData = serversQuery.data;
  const servers = useMemo(() => serversData ?? [], [serversData]);
  const existingNames = useMemo(() => new Set(servers.map((server) => server.name)), [servers]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<WorkspaceMcpServer | null>(null);
  const [deletingServer, setDeletingServer] = useState<WorkspaceMcpServer | null>(null);
  const [schemaServer, setSchemaServer] = useState<WorkspaceMcpServer | null>(null);
  const [credentialsServer, setCredentialsServer] = useState<WorkspaceMcpServer | null>(null);

  const liveServer = (server: WorkspaceMcpServer | null) =>
    server === null ? null : (servers.find((row) => row.id === server.id) ?? server);

  const handleSaveSchema = async (schema: McpCredentialField[]) => {
    if (!schemaServer) return;
    try {
      await updateServer.mutateAsync({
        serverId: schemaServer.id,
        credential_schema: schema,
      });
      setSchemaServer(null);
      toast.success(t(($) => $.mcp.updated_toast));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message ? error.message : t(($) => $.mcp.save_failed_toast),
      );
    }
  };

  const handleSaveCredentials = async (values: Record<string, string>) => {
    if (!credentialsServer) return;
    try {
      await setCredentials.mutateAsync({
        serverId: credentialsServer.id,
        values,
      });
      setCredentialsServer(null);
      toast.success(t(($) => $.mcp.credentials_saved_toast));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.credentials_failed_toast),
      );
    }
  };

  const dialogServer: ManagedMcpServer | null = editingServer
    ? {
        name: editingServer.name,
        config: editingServer.transport ? { type: editingServer.transport } : {},
        container: 'mcpServers',
        transport: editingServer.transport,
        enabled: true,
        masked: false,
      }
    : null;

  const handleSaveServer = async (name: string, config: Record<string, unknown>) => {
    try {
      if (editingServer) {
        await updateServer.mutateAsync({
          serverId: editingServer.id,
          name,
          config,
        });
      } else {
        await createServer.mutateAsync({ name, config });
      }
      toast.success(editingServer ? t(($) => $.mcp.updated_toast) : t(($) => $.mcp.added_toast));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message ? error.message : t(($) => $.mcp.save_failed_toast),
      );
      throw error;
    }
  };

  const handleDelete = async () => {
    if (!deletingServer) return;
    try {
      await deleteServer.mutateAsync(deletingServer.id);
      toast.success(t(($) => $.mcp.removed_toast));
      setDeletingServer(null);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.remove_failed_toast),
      );
    }
  };

  return (
    <SettingsTab title={t(($) => $.mcp.title)} description={t(($) => $.mcp.description)}>
      <SettingsSection
        title={t(($) => $.mcp.servers_title)}
        description={t(($) => $.mcp.write_only_note)}
        action={
          canManage ? (
            <Button
              size="sm"
              onClick={() => {
                setEditingServer(null);
                setEditorOpen(true);
              }}
            >
              <Plus className="h-4 w-4" />
              {t(($) => $.mcp.add_server)}
            </Button>
          ) : null
        }
      >
        <SettingsCard>
          {serversQuery.isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
            </div>
          ) : servers.length === 0 ? (
            <div className="px-4 py-8 text-center">
              <Server className="mx-auto h-5 w-5 text-muted-foreground" />
              <p className="mt-3 text-sm font-medium">{t(($) => $.mcp.empty_title)}</p>
              <p className="mx-auto mt-1 max-w-md text-xs leading-5 text-muted-foreground">
                {t(($) => $.mcp.empty_description)}
              </p>
            </div>
          ) : (
            <ul className="divide-y divide-border">
              {servers.map((server) => (
                <McpServerRow
                  key={server.id}
                  server={server}
                  canManage={canManage && server.source !== 'deployment'}
                  onEdit={() => {
                    setEditingServer(server);
                    setEditorOpen(true);
                  }}
                  onEditSchema={() => setSchemaServer(server)}
                  onConnect={() => setCredentialsServer(server)}
                  onDelete={() => setDeletingServer(server)}
                />
              ))}
            </ul>
          )}
        </SettingsCard>
        {/*
          The one thing an admin cannot discover from this screen. A library
          entry and a machine-local server meet by NAME in the daemon, and the
          merge replaces the machine entry wholesale rather than merging into
          it — so a library entry named like one the workspace configuration
          defaults switch on takes that entry's env with it. Said here because
          the library genuinely cannot see what any given machine defines.
        */}
        <p className="px-0.5 text-xs text-muted-foreground">
          {t(($) => $.mcp.name_collision_note)}
        </p>
        {!canManage ? (
          <p className="px-0.5 text-xs text-muted-foreground">{t(($) => $.mcp.admin_only_note)}</p>
        ) : null}
      </SettingsSection>

      <DeploymentMcpOffersSection wsId={wsId} canManage={canManage} />

      <McpCredentialSchemaDialog
        open={schemaServer !== null}
        server={liveServer(schemaServer)}
        saving={updateServer.isPending}
        onOpenChange={(open) => {
          if (!open) setSchemaServer(null);
        }}
        onSave={handleSaveSchema}
      />

      <McpCredentialsDialog
        open={credentialsServer !== null}
        server={liveServer(credentialsServer)}
        saving={setCredentials.isPending}
        onOpenChange={(open) => {
          if (!open) setCredentialsServer(null);
        }}
        onSave={handleSaveCredentials}
      />

      <McpServerDialog
        open={editorOpen}
        server={dialogServer}
        existingNames={existingNames}
        onOpenChange={setEditorOpen}
        onSave={handleSaveServer}
      />

      <AlertDialog
        open={deletingServer !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingServer(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.mcp.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.mcp.delete_description, {
                name: deletingServer?.name ?? '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteServer.isPending}>
              {t(($) => $.mcp.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void handleDelete();
              }}
              disabled={deleteServer.isPending}
            >
              {deleteServer.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t(($) => $.mcp.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

function McpServerRow({
  server,
  canManage,
  onEdit,
  onEditSchema,
  onConnect,
  onDelete,
}: {
  server: WorkspaceMcpServer;
  canManage: boolean;
  onEdit: () => void;
  onEditSchema: () => void;
  onConnect: () => void;
  onDelete: () => void;
}) {
  const { t } = useT('settings');
  const fields = server.credential_schema ?? [];
  const missing = server.missing_credentials ?? [];
  const provided = server.provided_credentials ?? [];
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{server.name}</span>
        <p className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
          {mcpTransportLabel(server.transport)}
          {server.source === 'deployment' ? (
            <Badge variant="outline" className="text-[10px]">
              {t(($) => $.mcp.deployment_badge)}
            </Badge>
          ) : null}
        </p>
        {/*
          The honest state of THIS viewer's copy, and nothing stronger. A
          supplied credential does not prove the server works — it only proves
          nothing is outstanding here — so the wording says exactly that and
          claims no green.
        */}
        {fields.length > 0 ? (
          missing.length > 0 ? (
            <p className="mt-1 text-xs leading-5 text-warning">
              {t(($) => $.mcp.credentials_missing_note, {
                fields: missing.join(', '),
              })}
            </p>
          ) : (
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              {t(($) => $.mcp.credentials_supplied_note, {
                count: provided.length,
              })}
            </p>
          )
        ) : null}
      </div>
      {/* Any member connects their own credentials — this is the one control
          on the screen that is deliberately not an admin control. */}
      {fields.length > 0 ? (
        <Button
          variant={missing.length > 0 ? 'default' : 'outline'}
          size="sm"
          className="shrink-0"
          onClick={onConnect}
        >
          <KeyRound className="h-4 w-4" />
          {missing.length > 0
            ? t(($) => $.mcp.credentials_connect)
            : t(($) => $.mcp.credentials_update)}
        </Button>
      ) : null}
      {canManage ? (
        <div className="flex shrink-0 items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            onClick={onEditSchema}
            aria-label={t(($) => $.mcp.schema_edit)}
          >
            <KeyRound className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onEdit}
            aria-label={t(($) => $.mcp.edit_server)}
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            aria-label={t(($) => $.mcp.remove_server)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ) : null}
    </li>
  );
}

function DeploymentMcpOffersSection({ wsId, canManage }: { wsId: string; canManage: boolean }) {
  const { t } = useT('settings');
  const offersQuery = useQuery({
    ...workspaceDeploymentMcpServersOptions(wsId),
    enabled: wsId !== '' && canManage,
  });
  const setEnabled = useSetWorkspaceDeploymentMcpServerEnabled(wsId);
  const offers = offersQuery.data;

  const toggle = async (server: DeploymentMcpServer, enabled: boolean) => {
    try {
      await setEnabled.mutateAsync({ serverId: server.id, enabled });
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.offer_failed_toast),
      );
    }
  };

  if (!canManage || offers === undefined) return null;

  return (
    <SettingsSection
      title={t(($) => $.mcp.offers_title)}
      description={t(($) => $.mcp.deployment_note)}
    >
      <SettingsCard>
        {offers.length === 0 ? (
          <p className="px-4 py-6 text-center text-xs text-muted-foreground">
            {t(($) => $.mcp.offers_empty)}
          </p>
        ) : (
          <ul className="divide-y divide-border">
            {offers.map((server) => (
              <li key={server.id} className="flex items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{server.name}</span>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {mcpTransportLabel(server.transport)}
                  </p>
                </div>
                <Switch
                  checked={server.enabled === true}
                  disabled={setEnabled.isPending}
                  aria-label={t(($) => $.mcp.offer_enable_aria, {
                    name: server.name,
                  })}
                  onCheckedChange={(checked) => void toggle(server, checked)}
                />
              </li>
            ))}
          </ul>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}

export function mcpTransportLabel(transport: string): string {
  switch (transport) {
    case 'stdio':
      return 'stdio';
    case 'http':
      return 'HTTP';
    case 'sse':
      return 'SSE';
    default:
      return transport || 'unknown';
  }
}
