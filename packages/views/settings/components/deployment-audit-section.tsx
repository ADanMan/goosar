'use client';

import { useQuery } from '@tanstack/react-query';
import { deploymentAuditOptions } from '@goosar/core/deployment/admin';
import { useT, useUiLocale } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';

export function DeploymentAuditSection() {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const { data: entries, isError } = useQuery(deploymentAuditOptions());

  const formatWhen = (raw?: string): string => {
    if (raw === undefined || raw === '') return '';
    const at = new Date(raw);
    return Number.isNaN(at.getTime()) ? raw : at.toLocaleString(uiLocale);
  };

  return (
    <SettingsSection
      title={t(($) => $.deployment.audit.title)}
      description={t(($) => $.deployment.audit.description)}
    >
      <SettingsCard>
        {isError ? (
          <div role="alert" className="px-4 py-3.5 text-sm text-destructive">
            {t(($) => $.deployment.audit.failed)}
          </div>
        ) : entries === undefined || entries.length === 0 ? (
          <div className="px-4 py-3.5 text-sm text-muted-foreground">
            {t(($) => $.deployment.audit.empty)}
          </div>
        ) : (
          entries.map((entry, index) => (
            <div key={entry.id || index} className="space-y-1 px-4 py-3">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                <span className="font-mono text-sm break-all">{entry.action}</span>
                <span className="text-xs text-muted-foreground">
                  {formatWhen(entry.created_at)}
                </span>
              </div>
              <div className="text-xs break-all text-muted-foreground">
                {t(($) => $.deployment.audit.row, {
                  actor:
                    entry.actor_user_id !== undefined && entry.actor_user_id !== ''
                      ? entry.actor_user_id
                      : t(($) => $.deployment.audit.actor_server),
                  target:
                    entry.target_id !== undefined && entry.target_id !== ''
                      ? `${entry.target_type}:${entry.target_id}`
                      : entry.target_type,
                })}
              </div>
            </div>
          ))
        )}
      </SettingsCard>
    </SettingsSection>
  );
}
