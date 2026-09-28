'use client';

import { useMemo, useState } from 'react';
import {
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table';
import { MoreHorizontal, Search, ShieldOff, UserRoundCog } from 'lucide-react';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
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
import { DataTable } from '@goosar/ui/components/ui/data-table';
import { DataTableColumnHeader } from '@goosar/ui/components/ui/data-table-column-header';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@goosar/ui/components/ui/empty';
import { describeApiFailure } from '@goosar/core/api';
import type {
  DeploymentAdminEntry,
  DeploymentAdminPending,
} from '@goosar/core/api/deployment-admin';
import { useAddDeploymentAdmin, useRemoveDeploymentAdmin } from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow, SettingsSection } from './settings-layout';
import { SectionNotice, type SectionNoticeState } from './section-notice';

export function DeploymentAdminsSection({
  admins,
  pending,
}: {
  admins: DeploymentAdminEntry[];
  pending: DeploymentAdminPending[];
}) {
  const { t } = useT('settings');
  const addAdmin = useAddDeploymentAdmin();
  const removeAdmin = useRemoveDeploymentAdmin();

  const [emailInput, setEmailInput] = useState('');
  const [search, setSearch] = useState('');
  const [notice, setNotice] = useState<SectionNoticeState | null>(null);
  const [confirmTarget, setConfirmTarget] = useState<DeploymentAdminEntry | null>(null);

  const pendingActionLabel = (pending: DeploymentAdminPending): string => {
    const target =
      pending.target_email !== undefined && pending.target_email !== ''
        ? pending.target_email
        : pending.target_user_id;
    switch (pending.action) {
      case 'revoke':
        return t(($) => $.deployment.admins.pending_revoke, { target });
      case 'grant':
      default:
        return t(($) => $.deployment.admins.pending_grant, { target });
    }
  };

  const presentError = (err: unknown) => {
    const failure = describeApiFailure(err);
    setNotice({
      tone: 'destructive',
      message: failure.message ?? t(($) => $.deployment.admins.toast_request_failed),
    });
  };

  const submitAdd = async () => {
    const email = emailInput.trim();
    if (email === '' || addAdmin.isPending) return;
    setNotice(null);
    try {
      const result = await addAdmin.mutateAsync(email);
      if (result.kind !== 'pending') {
        setNotice({
          tone: 'success',
          message: t(($) => $.deployment.admins.already_admin, {
            email: result.entry.email ?? email,
          }),
        });
      } else {
        setNotice({ tone: 'success', message: t(($) => $.deployment.admins.grant_filed) });
      }
      setEmailInput('');
    } catch (err) {
      presentError(err);
    }
  };

  const confirmRemove = async () => {
    const entry = confirmTarget;
    if (entry === null || removeAdmin.isPending) return;
    setNotice(null);
    try {
      await removeAdmin.mutateAsync(entry.user_id);
      setNotice({
        tone: 'success',
        message: t(($) => $.deployment.admins.revoke_filed, {
          name: entry.email ?? entry.name ?? entry.user_id,
        }),
      });
    } catch (err) {
      presentError(err);
    } finally {
      setConfirmTarget(null);
    }
  };

  const columns = useMemo<ColumnDef<DeploymentAdminEntry>[]>(
    () => [
      {
        id: 'name',
        accessorFn: (entry) => entry.name ?? entry.email ?? entry.user_id,
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.admins.col_name)} />
        ),
        cell: ({ row }) => (
          <span className="text-sm font-medium">
            {row.original.name ?? row.original.email ?? row.original.user_id}
          </span>
        ),
      },
      {
        id: 'email',
        accessorFn: (entry) => entry.email ?? '',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} label={t(($) => $.deployment.admins.col_email)} />
        ),
        cell: ({ row }) => (
          <span className="text-sm text-muted-foreground">{row.original.email ?? ''}</span>
        ),
      },
      {
        id: 'actions',
        header: '',
        size: 48,
        enableSorting: false,
        cell: ({ row }) => {
          const entry = row.original;
          const name = entry.email ?? entry.name ?? entry.user_id;
          return (
            <div className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t(($) => $.deployment.admins.row_actions_aria, { name })}
                    >
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={removeAdmin.isPending}
                    onClick={() => setConfirmTarget(entry)}
                  >
                    <ShieldOff />
                    {t(($) => $.deployment.admins.remove)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, removeAdmin.isPending],
  );

  const table = useReactTable({
    data: admins,
    columns,
    state: { globalFilter: search },
    onGlobalFilterChange: setSearch,
    getRowId: (row) => row.user_id,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  });

  const confirmName =
    confirmTarget !== null
      ? (confirmTarget.email ?? confirmTarget.name ?? confirmTarget.user_id)
      : '';

  return (
    <SettingsSection
      title={t(($) => $.deployment.admins.title)}
      description={t(($) => $.deployment.admins.description)}
    >
      {admins.length === 0 ? (
        <SettingsCard>
          <Empty className="border-none py-8">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UserRoundCog />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.deployment.admins.empty_title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.deployment.admins.empty)}</EmptyDescription>
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
              placeholder={t(($) => $.deployment.admins.search_placeholder)}
              aria-label={t(($) => $.deployment.admins.search_placeholder)}
              className="pl-8"
            />
          </div>
          <SettingsCard>
            <DataTable table={table} emptyMessage={t(($) => $.deployment.admins.no_matches)} />
          </SettingsCard>
        </div>
      )}

      <SettingsCard>
        <SettingsRow
          label={t(($) => $.deployment.admins.add_label)}
          description={t(($) => $.deployment.admins.add_hint)}
          size="text"
        >
          <div className="flex w-full items-center gap-2">
            <Input
              aria-label={t(($) => $.deployment.admins.email_aria)}
              type="email"
              value={emailInput}
              onChange={(e) => setEmailInput(e.target.value)}
              placeholder={t(($) => $.deployment.admins.email_placeholder)}
              autoComplete="off"
            />
            <Button
              size="sm"
              onClick={submitAdd}
              disabled={addAdmin.isPending || emailInput.trim() === ''}
            >
              {t(($) => $.deployment.admins.add)}
            </Button>
          </div>
        </SettingsRow>
      </SettingsCard>

      <SectionNotice
        notice={notice}
        onDismiss={() => setNotice(null)}
        dismissLabel={t(($) => $.deployment.admins.notice_dismiss)}
      />

      {pending.length > 0 && (
        <SettingsCard>
          {pending.map((request) => (
            <div key={request.request_id} className="space-y-2 px-4 py-3.5">
              <div className="flex items-center gap-2">
                <Badge variant="outline">{t(($) => $.deployment.admins.pending_status)}</Badge>
                <span className="text-sm font-medium">{pendingActionLabel(request)}</span>
              </div>
              <div className="text-xs leading-5 text-muted-foreground">
                {t(($) => $.deployment.admins.confirm_instruction)}
              </div>
              <code className="block overflow-x-auto rounded bg-muted px-2 py-1.5 font-mono text-xs">
                {request.confirm_hint}
              </code>
            </div>
          ))}
        </SettingsCard>
      )}

      <AlertDialog
        open={confirmTarget !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.deployment.admins.confirm_remove_title, { name: confirmName })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.admins.confirm_remove_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.deployment.admins.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={removeAdmin.isPending}
              onClick={confirmRemove}
            >
              {t(($) => $.deployment.admins.remove)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
