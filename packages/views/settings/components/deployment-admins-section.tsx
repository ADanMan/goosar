'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { describeApiFailure } from '@goosar/core/api';
import type {
  DeploymentAdminEntry,
  DeploymentAdminPending,
} from '@goosar/core/api/deployment-admin';
import { useAddDeploymentAdmin, useRemoveDeploymentAdmin } from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow, SettingsSection } from './settings-layout';

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
  const [lastError, setLastError] = useState<string | null>(null);

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
    setLastError(failure.message ?? t(($) => $.deployment.admins.toast_request_failed));
  };

  const submitAdd = async () => {
    const email = emailInput.trim();
    if (email === '' || addAdmin.isPending) return;
    setLastError(null);
    try {
      const result = await addAdmin.mutateAsync(email);
      if (result.kind !== 'pending') {
        toast.success(
          t(($) => $.deployment.admins.already_admin, {
            email: result.entry.email ?? email,
          }),
        );
      }
      setEmailInput('');
    } catch (err) {
      presentError(err);
    }
  };

  const submitRemove = async (entry: DeploymentAdminEntry) => {
    if (removeAdmin.isPending) return;
    setLastError(null);
    try {
      await removeAdmin.mutateAsync(entry.user_id);
    } catch (err) {
      presentError(err);
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.deployment.admins.title)}
      description={t(($) => $.deployment.admins.description)}
    >
      <SettingsCard>
        {admins.length === 0 ? (
          <div className="px-4 py-3.5 text-sm text-muted-foreground">
            {t(($) => $.deployment.admins.empty)}
          </div>
        ) : (
          admins.map((entry) => (
            <SettingsRow
              key={entry.user_id}
              label={entry.name ?? entry.email ?? entry.user_id}
              description={entry.email}
            >
              <Button
                variant="ghost"
                size="sm"
                onClick={() => submitRemove(entry)}
                disabled={removeAdmin.isPending}
                aria-label={t(($) => $.deployment.admins.remove_aria, {
                  name: entry.email ?? entry.name ?? entry.user_id,
                })}
              >
                {t(($) => $.deployment.admins.remove)}
              </Button>
            </SettingsRow>
          ))
        )}
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

      {lastError !== null && (
        <p role="alert" className="px-0.5 text-xs text-destructive">
          {lastError}
        </p>
      )}

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
    </SettingsSection>
  );
}
