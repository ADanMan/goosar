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
import { FileClock, Search } from 'lucide-react';
import { deploymentAuditOptions } from '@goosar/core/deployment/admin';
import type { AdminAuditEntry } from '@goosar/core/api/deployment-admin';
import { Alert, AlertDescription } from '@goosar/ui/components/ui/alert';
import { DataTable } from '@goosar/ui/components/ui/data-table';
import { DataTableColumnHeader } from '@goosar/ui/components/ui/data-table-column-header';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@goosar/ui/components/ui/empty';
import { Input } from '@goosar/ui/components/ui/input';
import { useT, useUiLocale } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';

export function DeploymentAuditSection() {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const { data: entries, isError } = useQuery(deploymentAuditOptions());
  const [search, setSearch] = useState('');

  const formatWhen = (raw?: string): string => {
    if (raw === undefined || raw === '') return '';
    const at = new Date(raw);
    return Number.isNaN(at.getTime()) ? raw : at.toLocaleString(uiLocale);
  };

  const describeRow = (entry: AdminAuditEntry): string =>
    t(($) => $.deployment.audit.row, {
      actor:
        entry.actor_user_id !== undefined && entry.actor_user_id !== ''
          ? entry.actor_user_id
          : t(($) => $.deployment.audit.actor_server),
      target:
        entry.target_id !== undefined && entry.target_id !== ''
          ? `${entry.target_type}:${entry.target_id}`
          : entry.target_type,
    });

  const columns = useMemo<ColumnDef<AdminAuditEntry>[]>(
    () => [
      {
        id: 'action',
        accessorKey: 'action',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.audit.col_action)} />
        ),
        cell: ({ row }) => <span className="font-mono text-sm break-all">{row.original.action}</span>,
      },
      {
        id: 'summary',
        accessorFn: (entry) => describeRow(entry),
        header: t(($) => $.deployment.audit.col_summary),
        cell: ({ getValue }) => (
          <span className="text-xs break-all text-muted-foreground">{getValue<string>()}</span>
        ),
      },
      {
        id: 'created_at',
        accessorFn: (entry) => entry.created_at ?? '',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.audit.col_when)} />
        ),
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">{formatWhen(row.original.created_at)}</span>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, uiLocale],
  );

  const table = useReactTable({
    data: entries ?? [],
    columns,
    state: { globalFilter: search },
    onGlobalFilterChange: setSearch,
    getRowId: (row, index) => row.id || String(index),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  });

  return (
    <SettingsSection
      title={t(($) => $.deployment.audit.title)}
      description={t(($) => $.deployment.audit.description)}
    >
      {isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t(($) => $.deployment.audit.failed)}</AlertDescription>
        </Alert>
      ) : entries !== undefined && entries.length === 0 ? (
        <SettingsCard>
          <Empty className="border-none py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FileClock />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.deployment.audit.empty_title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.deployment.audit.empty)}</EmptyDescription>
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
              placeholder={t(($) => $.deployment.audit.search_placeholder)}
              aria-label={t(($) => $.deployment.audit.search_placeholder)}
              className="pl-8"
            />
          </div>
          <SettingsCard>
            <DataTable table={table} emptyMessage={t(($) => $.deployment.audit.no_matches)} />
          </SettingsCard>
        </div>
      )}
    </SettingsSection>
  );
}
