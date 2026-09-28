'use client';

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table';
import { KeyRound, Loader2, MoreHorizontal, Pencil, Plus, Search, Server, Trash2 } from 'lucide-react';
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
import { DataTable } from '@goosar/ui/components/ui/data-table';
import { DataTableColumnHeader } from '@goosar/ui/components/ui/data-table-column-header';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@goosar/ui/components/ui/empty';
import { Input } from '@goosar/ui/components/ui/input';
import { McpServerDialog } from '../../agents/components/tabs/mcp-server-dialog';
import { McpCredentialSchemaDialog } from './mcp-credential-schema-dialog';
import type { ManagedMcpServer } from '../../agents/components/tabs/mcp-config-model';
import { useT } from '../../i18n';
import { mcpTransportLabel } from './mcp-tab';
import { SettingsCard, SettingsSection } from './settings-layout';
import { SectionNotice, type SectionNoticeState } from './section-notice';

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
  const [search, setSearch] = useState('');
  const [notice, setNotice] = useState<SectionNoticeState | null>(null);
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

  const failureMessage = (error: unknown, fallback: string): string =>
    error instanceof Error && error.message ? error.message : fallback;

  const handleSaveServer = async (name: string, config: Record<string, unknown>) => {
    try {
      if (editingServer) {
        await updateServer.mutateAsync({ serverId: editingServer.id, name, config });
      } else {
        await createServer.mutateAsync({ name, config });
      }
      setNotice({
        tone: 'success',
        message: editingServer
          ? t(($) => $.deployment.mcp.updated_toast)
          : t(($) => $.deployment.mcp.added_toast),
      });
    } catch (error) {
      setNotice({
        tone: 'destructive',
        message: failureMessage(error, t(($) => $.deployment.mcp.save_failed_toast)),
      });
      throw error;
    }
  };

  const handleSaveSchema = async (schema: McpCredentialField[]) => {
    if (!liveSchemaServer) return;
    try {
      await updateServer.mutateAsync({ serverId: liveSchemaServer.id, credential_schema: schema });
      setSchemaServer(null);
      setNotice({ tone: 'success', message: t(($) => $.deployment.mcp.updated_toast) });
    } catch (error) {
      setNotice({
        tone: 'destructive',
        message: failureMessage(error, t(($) => $.deployment.mcp.save_failed_toast)),
      });
      throw error;
    }
  };

  const handleDelete = async () => {
    if (!deletingServer) return;
    try {
      await deleteServer.mutateAsync(deletingServer.id);
      setNotice({ tone: 'success', message: t(($) => $.deployment.mcp.removed_toast) });
      setDeletingServer(null);
    } catch (error) {
      setNotice({
        tone: 'destructive',
        message: failureMessage(error, t(($) => $.deployment.mcp.remove_failed_toast)),
      });
    }
  };

  const reachLabel = (server: DeploymentMcpServer): string | null => {
    const reach = server.enabled_workspaces;
    if (reach === undefined) return null;
    return reach === 0
      ? t(($) => $.deployment.mcp.not_enabled_anywhere)
      : t(($) => $.deployment.mcp.enabled_workspaces, { count: reach });
  };

  const columns = useMemo<ColumnDef<DeploymentMcpServer>[]>(
    () => [
      {
        id: 'name',
        accessorKey: 'name',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.mcp.col_name)} />
        ),
        cell: ({ row }) => <span className="text-sm font-medium break-all">{row.original.name}</span>,
      },
      {
        id: 'transport',
        accessorFn: (server) => mcpTransportLabel(server.transport),
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.mcp.col_transport)} />
        ),
        cell: ({ row }) => {
          const reach = reachLabel(row.original);
          return (
            <span className="text-xs text-muted-foreground">
              {mcpTransportLabel(row.original.transport)}
              {reach ? ` · ${reach}` : ''}
            </span>
          );
        },
      },
      {
        id: 'actions',
        header: '',
        size: 48,
        enableSorting: false,
        cell: ({ row }) => {
          const server = row.original;
          return (
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t(($) => $.deployment.mcp.row_actions_aria, { name: server.name })}
                    >
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => setSchemaServer(server)}>
                    <KeyRound />
                    {t(($) => $.mcp.schema_edit)}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      setEditingServer(server);
                      setEditorOpen(true);
                    }}
                  >
                    <Pencil />
                    {t(($) => $.deployment.mcp.edit_server)}
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" onClick={() => setDeletingServer(server)}>
                    <Trash2 />
                    {t(($) => $.deployment.mcp.remove_server)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t],
  );

  const table = useReactTable({
    data: servers,
    columns,
    state: { globalFilter: search },
    onGlobalFilterChange: setSearch,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  });

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
      {serversQuery.isLoading ? (
        <SettingsCard>
          <div className="flex items-center justify-center py-8 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
          </div>
        </SettingsCard>
      ) : servers.length === 0 ? (
        <SettingsCard>
          <Empty className="border-none py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Server />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.deployment.mcp.empty_title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.deployment.mcp.empty_description)}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </SettingsCard>
      ) : (
        <div className="space-y-2">
          <div className="relative sm:w-72">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t(($) => $.deployment.mcp.search_placeholder)}
              aria-label={t(($) => $.deployment.mcp.search_placeholder)}
              className="pl-8"
            />
          </div>
          <SettingsCard>
            <DataTable table={table} emptyMessage={t(($) => $.deployment.mcp.no_matches)} />
          </SettingsCard>
        </div>
      )}
      <p className="px-0.5 text-xs text-muted-foreground">
        {t(($) => $.deployment.mcp.write_only_note)}
      </p>

      <SectionNotice
        notice={notice}
        onDismiss={() => setNotice(null)}
        dismissLabel={t(($) => $.deployment.mcp.notice_dismiss)}
      />

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
              variant="destructive"
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
