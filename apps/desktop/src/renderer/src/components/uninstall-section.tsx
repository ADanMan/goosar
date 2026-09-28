import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { AlertCircle, AlertTriangle, Check, Info } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { Checkbox } from '@goosar/ui/components/ui/checkbox';
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
import { cn } from '@goosar/ui/lib/utils';
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  TypedConfirmDialog,
} from '@goosar/views/settings';
import { useT } from '@goosar/views/i18n';

import {
  formatBytes,
  totalBytes,
  type KeptItem,
  type ManualStep,
  type UninstallItem,
  type UninstallOutcome,
  type UninstallPlan,
} from '../../../shared/uninstall-types';

function ItemRow({ item }: { item: UninstallItem }) {
  return (
    <li className="flex items-baseline justify-between gap-4 py-1.5">
      <div className="min-w-0">
        <p className="text-sm">{item.label}</p>
        <p className="mt-0.5 break-all font-mono text-xs text-muted-foreground">{item.path}</p>
      </div>
      <span className="shrink-0 font-mono text-xs text-muted-foreground">
        {formatBytes(item.bytes)}
      </span>
    </li>
  );
}

function ItemList({ items }: { items: readonly UninstallItem[] }) {
  return (
    <ul className="divide-y divide-surface-border">
      {items.map((item) => (
        <ItemRow key={item.id} item={item} />
      ))}
    </ul>
  );
}

function KeptList({ items }: { items: readonly KeptItem[] }) {
  return (
    <ul className="divide-y divide-surface-border">
      {items.map((item) => (
        <li key={item.path} className="py-1.5">
          <p className="break-all font-mono text-xs">{item.path}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{item.reason}</p>
        </li>
      ))}
    </ul>
  );
}

function ManualStepList({ steps }: { steps: readonly ManualStep[] }) {
  return (
    <ul className="divide-y divide-surface-border">
      {steps.map((step) => (
        <li key={step.path} className="py-1.5">
          <p className="break-all font-mono text-xs">{step.path}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">{step.instruction}</p>
        </li>
      ))}
    </ul>
  );
}

function Banner({
  tone,
  icon,
  title,
  children,
}: {
  tone: 'error' | 'warning' | 'neutral';
  icon: ReactNode;
  title: string;
  children?: ReactNode;
}) {
  return (
    <div
      className={cn(
        'flex items-start gap-3 rounded-lg border px-4 py-3',
        tone === 'error' && 'border-destructive/40 bg-destructive/5',
        tone === 'warning' && 'border-warning/40 bg-warning/5',
        tone === 'neutral' && 'bg-muted/30',
      )}
    >
      {icon}
      <div className="min-w-0 flex-1">
        <p className={cn('text-sm font-medium', tone === 'error' && 'text-destructive')}>{title}</p>
        {children}
      </div>
    </div>
  );
}

function OutcomeReport({ outcome }: { outcome: UninstallOutcome }) {
  const { t } = useT('settings');

  if (outcome.status === 'blocked') {
    return (
      <Banner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title={t(($) => $.desktop.uninstall.outcome_blocked_title)}
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">
          {outcome.daemon.detail ?? t(($) => $.desktop.uninstall.outcome_blocked_fallback)}
        </p>
      </Banner>
    );
  }

  if (outcome.status === 'partial') {
    return (
      <Banner
        tone="warning"
        icon={<AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
        title={t(($) => $.desktop.uninstall.outcome_partial_title, {
          count: outcome.removed.length,
        })}
      >
        <ul className="mt-2 divide-y divide-surface-border">
          {outcome.failed.map((failure) => (
            <li key={failure.item.id} className="py-1.5">
              <p className="break-all font-mono text-xs">{failure.item.path}</p>
              <p className="mt-0.5 break-words text-xs text-muted-foreground">{failure.message}</p>
            </li>
          ))}
        </ul>
        <p className="mt-2 text-xs text-muted-foreground">
          {t(($) => $.desktop.uninstall.outcome_partial_hint)}
        </p>
      </Banner>
    );
  }

  if (outcome.status === 'nothing_to_remove') {
    return (
      <Banner
        tone="neutral"
        icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
        title={t(($) => $.desktop.uninstall.outcome_nothing_to_remove_title)}
      />
    );
  }

  return (
    <Banner
      tone="neutral"
      icon={<Check className="mt-0.5 size-4 shrink-0 text-success" />}
      title={t(($) => $.desktop.uninstall.outcome_success_title, {
        count: outcome.removed.length,
      })}
    >
      {outcome.manualSteps.length > 0 && (
        <div className="mt-2">
          <p className="text-xs text-muted-foreground">
            {t(($) => $.desktop.uninstall.outcome_success_manual_note)}
          </p>
          <ManualStepList steps={outcome.manualSteps} />
        </div>
      )}
    </Banner>
  );
}

export function UninstallSection() {
  const { t } = useT('settings');
  const [plan, setPlan] = useState<UninstallPlan | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [planning, setPlanning] = useState(false);
  const [includeUserData, setIncludeUserData] = useState(false);
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<UninstallOutcome | null>(null);
  const [runError, setRunError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const review = useCallback(async () => {
    setPlanning(true);
    setPlanError(null);
    setOutcome(null);
    setRunError(null);
    try {
      setPlan(await window.desktopAPI.planUninstall());
    } catch (err) {
      setPlan(null);
      setPlanError(err instanceof Error ? err.message : String(err));
    } finally {
      setPlanning(false);
    }
  }, []);

  const remove = useCallback(async () => {
    setRunning(true);
    setRunError(null);
    setOutcome(null);
    try {
      setOutcome(await window.desktopAPI.performUninstall({ includeUserData }));
      setPlan(null);
    } catch (err) {
      setRunError(err instanceof Error ? err.message : String(err));
    } finally {
      setRunning(false);
    }
  }, [includeUserData]);

  // The destructive button only opens a confirmation; performUninstall runs
  // from inside the confirm dialogs below, never straight from the click.
  const confirmRemove = () => {
    setConfirmOpen(false);
    void remove();
  };

  const selected = useMemo(() => {
    if (!plan) return [];
    return includeUserData ? [...plan.software, ...plan.userData] : plan.software;
  }, [plan, includeUserData]);

  const hasAnything = plan !== null && plan.software.length + plan.userData.length > 0;

  return (
    <SettingsSection
      title={t(($) => $.desktop.uninstall.title)}
      description={t(($) => $.desktop.uninstall.description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.uninstall.review_label)}
          description={t(($) => $.desktop.uninstall.review_description)}
        >
          <Button variant="outline" onClick={review} disabled={planning || running}>
            {planning
              ? t(($) => $.desktop.uninstall.review_button_loading)
              : t(($) => $.desktop.uninstall.review_button)}
          </Button>
        </SettingsRow>
      </SettingsCard>

      {planError !== null && (
        <Banner
          tone="error"
          icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
          title={t(($) => $.desktop.uninstall.plan_error_title)}
        >
          <p className="mt-0.5 break-words text-sm text-muted-foreground">{planError}</p>
        </Banner>
      )}

      {plan !== null && !hasAnything && (
        <Banner
          tone="neutral"
          icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
          title={t(($) => $.desktop.uninstall.nothing_installed_title)}
        >
          {plan.manualSteps.length > 0 && (
            <div className="mt-2">
              <ManualStepList steps={plan.manualSteps} />
            </div>
          )}
        </Banner>
      )}

      {plan !== null && hasAnything && (
        <SettingsCard>
          <div className="px-4 py-3">
            <p className="text-sm font-medium">{t(($) => $.desktop.uninstall.will_be_removed)}</p>
            <ItemList items={plan.software} />
          </div>

          {plan.userData.length > 0 && (
            <div className="px-4 py-3">
              <label className="flex cursor-pointer items-start gap-2">
                <Checkbox
                  className="mt-0.5"
                  checked={includeUserData}
                  onCheckedChange={(next) => setIncludeUserData(next === true)}
                  disabled={running}
                />
                <span className="min-w-0 leading-5">
                  <span className="text-sm font-medium">
                    {t(($) => $.desktop.uninstall.keep_user_data_label)}
                  </span>
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    {t(($) => $.desktop.uninstall.keep_user_data_description)}
                  </span>
                </span>
              </label>
              <div className={cn(!includeUserData && 'opacity-60')}>
                <ItemList items={plan.userData} />
              </div>
            </div>
          )}

          {plan.kept.length > 0 && (
            <div className="px-4 py-3">
              <p className="text-sm font-medium">{t(($) => $.desktop.uninstall.left_alone)}</p>
              <KeptList items={plan.kept} />
            </div>
          )}

          {plan.manualSteps.length > 0 && (
            <div className="px-4 py-3">
              <p className="text-sm font-medium">
                {t(($) => $.desktop.uninstall.manual_steps_label)}
              </p>
              <ManualStepList steps={plan.manualSteps} />
            </div>
          )}

          <SettingsRow
            label={t(($) => $.desktop.uninstall.remove_row_label)}
            description={t(($) => $.desktop.uninstall.remove_row_description)}
          >
            <Button
              variant="destructive"
              onClick={() => setConfirmOpen(true)}
              disabled={running || selected.length === 0}
            >
              {running
                ? t(($) => $.desktop.uninstall.remove_button_loading)
                : t(($) => $.desktop.uninstall.remove_button, {
                    count: selected.length,
                    size: formatBytes(totalBytes(selected)),
                  })}
            </Button>
          </SettingsRow>
        </SettingsCard>
      )}

      {runError !== null && (
        <Banner
          tone="error"
          icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
          title={t(($) => $.desktop.uninstall.run_error_title)}
        >
          <p className="mt-0.5 break-words text-sm text-muted-foreground">{runError}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            {t(($) => $.desktop.uninstall.run_error_body)}
          </p>
        </Banner>
      )}

      {outcome !== null && <OutcomeReport outcome={outcome} />}

      {/* Deleting the daemon's own config is recoverable (reinstalling rebuilds
          it), so a plain confirm is enough. Deleting the user's own data is not,
          so that path is gated by TypedConfirmDialog below instead. */}
      {confirmOpen && !includeUserData && (
        <AlertDialog
          open
          onOpenChange={(next) => {
            if (!next) setConfirmOpen(false);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.desktop.uninstall.confirm_daemon_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.desktop.uninstall.confirm_daemon_description, {
                  size: formatBytes(totalBytes(selected)),
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.desktop.uninstall.confirm_cancel)}</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={confirmRemove}>
                {t(($) => $.desktop.uninstall.confirm_action)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {confirmOpen && includeUserData && (
        <TypedConfirmDialog
          inputId="uninstall-confirm-typed"
          title={t(($) => $.desktop.uninstall.confirm_user_data_title)}
          description={t(($) => $.desktop.uninstall.confirm_user_data_description, {
            size: formatBytes(totalBytes(selected)),
          })}
          target={t(($) => $.desktop.uninstall.confirm_word)}
          unavailableNote=""
          confirmLabel={t(($) => $.desktop.uninstall.confirm_action)}
          cancelLabel={t(($) => $.desktop.uninstall.confirm_cancel)}
          loading={running}
          onClose={() => setConfirmOpen(false)}
          onConfirm={confirmRemove}
        />
      )}
    </SettingsSection>
  );
}
