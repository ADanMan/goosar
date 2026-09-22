'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { KeyRound, Loader2, Pencil, Plus, Server, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import type { DeploymentMcpServer } from '@goosar/core/api/deployment-mcp';
import type { McpCredentialField } from '@goosar/core/api/workspace-mcp';
import {
  deploymentMcpServersOptions,
  useCreateDeploymentMcpServer,
  useDeleteDeploymentMcpServer,
  useUpdateDeploymentMcpServer,
} from '@goosar/core/deployment/mcp-servers';
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
import { Button } from '@goosar/ui/components/ui/button';
import { McpServerDialog } from '../../agents/components/tabs/mcp-server-dialog';
import { McpCredentialSchemaDialog } from './mcp-credential-schema-dialog';
import type { ManagedMcpServer } from '../../agents/components/tabs/mcp-config-model';
import { useT } from '../../i18n';
import { mcpTransportLabel } from './mcp-tab';
import { SettingsCard, SettingsSection } from './settings-layout';

export function DeploymentMcpSection() {
  const { t } = useT('settings');
  const serversQuery = useQuery(deploymentMcpServersOptions());
  const createServer = useCreateDeploymentMcpServer();
  const updateServer = useUpdateDeploymentMcpServer();
  const deleteServer = useDeleteDeploymentMcpServer();

  const serversData = serversQuery.data;
  const servers = useMemo(() => serversData ?? [], [serversData]);
  const existingNames = useMemo(() => new Set(servers.map((server) => server.name)), [servers]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<DeploymentMcpServer | null>(null);
  const [deletingServer, setDeletingServer] = useState<DeploymentMcpServer | null>(null);
  const [schemaServer, setSchemaServer] = useState<DeploymentMcpServer | null>(null);
  const liveSchemaServer =
    schemaServer === null
      ? null
      : (servers.find((server) => server.id === schemaServer.id) ?? schemaServer);

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
      toast.success(
        editingServer
          ? t(($) => $.deployment.mcp.updated_toast)
          : t(($) => $.deployment.mcp.added_toast),
      );
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.deployment.mcp.save_failed_toast),
      );
      throw error;
    }
  };

  const handleSaveSchema = async (schema: McpCredentialField[]) => {
    if (!liveSchemaServer) return;
    try {
      await updateServer.mutateAsync({
        serverId: liveSchemaServer.id,
        credential_schema: schema,
      });
      setSchemaServer(null);
      toast.success(t(($) => $.deployment.mcp.updated_toast));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.deployment.mcp.save_failed_toast),
      );
      throw error;
    }
  };

  const handleDelete = async () => {
    if (!deletingServer) return;
    try {
      await deleteServer.mutateAsync(deletingServer.id);
      toast.success(t(($) => $.deployment.mcp.removed_toast));
      setDeletingServer(null);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.deployment.mcp.remove_failed_toast),
      );
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.deployment.mcp.title)}
      description={t(($) => $.deployment.mcp.description)}
      action={
        <Button
          size="sm"
          onClick={() => {
            setEditingServer(null);
            setEditorOpen(true);
          }}
        >
          <Plus className="h-4 w-4" />
          {t(($) => $.deployment.mcp.add_server)}
        </Button>
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
            <p className="mt-3 text-sm font-medium">{t(($) => $.deployment.mcp.empty_title)}</p>
            <p className="mx-auto mt-1 max-w-md text-xs leading-5 text-muted-foreground">
              {t(($) => $.deployment.mcp.empty_description)}
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-border">
            {servers.map((server) => (
              <DeploymentMcpRow
                key={server.id}
                server={server}
                onEditSchema={() => setSchemaServer(server)}
                onEdit={() => {
                  setEditingServer(server);
                  setEditorOpen(true);
                }}
                onDelete={() => setDeletingServer(server)}
              />
            ))}
          </ul>
        )}
      </SettingsCard>
      <p className="px-0.5 text-xs text-muted-foreground">
        {t(($) => $.deployment.mcp.write_only_note)}
      </p>

      <McpServerDialog
        open={editorOpen}
        server={dialogServer}
        existingNames={existingNames}
        onOpenChange={setEditorOpen}
        onSave={handleSaveServer}
      />

      <McpCredentialSchemaDialog
        open={schemaServer !== null}
        server={liveSchemaServer}
        saving={updateServer.isPending}
        onOpenChange={(open) => {
          if (!open) setSchemaServer(null);
        }}
        onSave={handleSaveSchema}
      />

      <AlertDialog
        open={deletingServer !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingServer(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.deployment.mcp.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.mcp.delete_description, {
                name: deletingServer?.name ?? '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteServer.isPending}>
              {t(($) => $.deployment.mcp.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void handleDelete();
              }}
              disabled={deleteServer.isPending}
            >
              {deleteServer.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {t(($) => $.deployment.mcp.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}

function DeploymentMcpRow({
  server,
  onEditSchema,
  onEdit,
  onDelete,
}: {
  server: DeploymentMcpServer;
  onEditSchema: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT('settings');
  const reach = server.enabled_workspaces;
  const reachLabel =
    reach === undefined
      ? null
      : reach === 0
        ? t(($) => $.deployment.mcp.not_enabled_anywhere)
        : t(($) => $.deployment.mcp.enabled_workspaces, { count: reach });
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{server.name}</span>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {mcpTransportLabel(server.transport)}
          {reachLabel ? ` · ${reachLabel}` : ''}
        </p>
      </div>
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
          aria-label={t(($) => $.deployment.mcp.edit_server)}
        >
          <Pencil className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={onDelete}
          aria-label={t(($) => $.deployment.mcp.remove_server)}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
    </li>
  );
}
