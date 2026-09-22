'use client';

import { Loader2, PackageCheck, RefreshCw, TriangleAlert } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { useT } from '../i18n';
import type { ProvisioningStatus } from './provisioning-status';

export function ProvisioningReminder({
  status,
  onRetry,
}: {
  status: ProvisioningStatus | null;
  onRetry: () => void | Promise<void>;
}) {
  const { t } = useT('onboarding');

  if (!status) return null;
  if (status.configured !== true) return null;

  const { summary, state } = status;
  const platformUncovered = status.reasonCode === 'provisioning_platform_uncovered';
  const failed = summary.failed > 0;
  const delivered =
    !platformUncovered && state === 'ok' && !failed && summary.installed >= summary.total;
  if (delivered) return null;

  const syncing = state === 'syncing';
  if (syncing && !failed && summary.total === 0) return null;
  const failedPackages = (status.packages ?? []).filter((p) => p.state === 'fail');
  const reasonText = (code: string | undefined): string => {
    switch (code) {
      case 'provisioning_download_failed':
        return t(($) => $.provisioning_reminder.reason_download_failed);
      case 'provisioning_sha256_mismatch':
        return t(($) => $.provisioning_reminder.reason_sha256_mismatch);
      case 'provisioning_extract_failed':
        return t(($) => $.provisioning_reminder.reason_extract_failed);
      default:
        return t(($) => $.provisioning_reminder.reason_unknown);
    }
  };
  const body = platformUncovered
    ? t(($) => $.provisioning_reminder.platform_uncovered, {
        platform: status.platform ?? '?',
        platforms: (status.platformsAvailable ?? []).filter((p) => p !== '*').join(', ') || '*',
      })
    : state === 'fail' || failed
      ? t(($) => $.provisioning_reminder.failed)
      : syncing
        ? t(($) => $.provisioning_reminder.syncing, {
            installed: summary.installed,
            total: summary.total,
          })
        : 
          t(($) => $.provisioning_reminder.not_delivered);

  return (
    <div
      role="status"
      className="pointer-events-none fixed bottom-4 left-4 z-40 max-w-[320px]"
    >
      <div className="pointer-events-auto flex items-start gap-2.5 rounded-lg border bg-card p-3 shadow-lg">
        {syncing ? (
          <Loader2
            className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-muted-foreground"
            aria-hidden
          />
        ) : state === 'fail' || failed ? (
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
        ) : (
          <PackageCheck className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        )}
        <div className="min-w-0 flex-1">
          <p className="text-[13px] font-medium text-foreground">
            {t(($) => $.provisioning_reminder.title)}
          </p>
          <p className="mt-0.5 text-[12px] leading-[1.5] text-muted-foreground">{body}</p>
          {failedPackages.length > 0 ? (
            <ul className="mt-1 list-disc pl-4 text-[12px] leading-[1.5] text-muted-foreground">
              {failedPackages.map((p) => (
                <li key={`${p.type}:${p.name}`}>
                  {t(($) => $.provisioning_reminder.failed_package, {
                    name: p.name,
                    reason: reasonText(p.reasonCode),
                  })}
                </li>
              ))}
            </ul>
          ) : null}
          {/* No retry while a sync is running, and none when nothing on this
              machine can change the outcome (#574). */}
          {syncing || platformUncovered ? null : (
            <Button size="sm" variant="outline" className="mt-2" onClick={() => void onRetry()}>
              <RefreshCw className="h-3.5 w-3.5" aria-hidden />
              {t(($) => $.provisioning_reminder.retry)}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

ProvisioningReminder.displayName = 'ProvisioningReminder';
