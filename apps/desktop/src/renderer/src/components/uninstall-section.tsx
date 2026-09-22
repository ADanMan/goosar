import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { AlertCircle, AlertTriangle, Check, Info } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { Checkbox } from '@goosar/ui/components/ui/checkbox';
import { cn } from '@goosar/ui/lib/utils';
import { SettingsCard, SettingsRow, SettingsSection } from '@goosar/views/settings';

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
  if (outcome.status === 'blocked') {
    return (
      <Banner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title="Nothing was removed"
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">
          {outcome.daemon.detail ??
            'The local daemon is still running, and deleting its files while it holds them open would leave a worse mess than not deleting them.'}
        </p>
      </Banner>
    );
  }

  if (outcome.status === 'partial') {
    return (
      <Banner
        tone="warning"
        icon={<AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
        title={`Removed ${outcome.removed.length}. Some of it is still on this computer.`}
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
          Remove these by hand, or try again once whatever is holding them has let go.
        </p>
      </Banner>
    );
  }

  if (outcome.status === 'nothing_to_remove') {
    return (
      <Banner
        tone="neutral"
        icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
        title="There was nothing left to remove."
      />
    );
  }

  return (
    <Banner
      tone="neutral"
      icon={<Check className="mt-0.5 size-4 shrink-0 text-success" />}
      title={`Removed ${outcome.removed.length} ${outcome.removed.length === 1 ? 'item' : 'items'}.`}
    >
      {outcome.manualSteps.length > 0 && (
        <div className="mt-2">
          <p className="text-xs text-muted-foreground">
            One thing is left, and the app cannot do it while it is running:
          </p>
          <ManualStepList steps={outcome.manualSteps} />
        </div>
      )}
    </Banner>
  );
}

export function UninstallSection() {
  const [plan, setPlan] = useState<UninstallPlan | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [planning, setPlanning] = useState(false);
  const [includeUserData, setIncludeUserData] = useState(false);
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<UninstallOutcome | null>(null);
  const [runError, setRunError] = useState<string | null>(null);

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

  const selected = useMemo(() => {
    if (!plan) return [];
    return includeUserData ? [...plan.software, ...plan.userData] : plan.software;
  }, [plan, includeUserData]);

  const hasAnything = plan !== null && plan.software.length + plan.userData.length > 0;

  return (
    <SettingsSection
      title="Uninstall"
      description="Remove what Goosar put on this computer. Nothing is deleted until you have seen the list."
    >
      <SettingsCard>
        <SettingsRow
          label="Review what would be removed"
          description="Reads the disk and lists every file and folder this app created, with its size. Nothing is deleted by looking."
        >
          <Button variant="outline" onClick={review} disabled={planning || running}>
            {planning ? 'Reading…' : 'Show what would be removed'}
          </Button>
        </SettingsRow>
      </SettingsCard>

      {planError !== null && (
        <Banner
          tone="error"
          icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
          title="Could not read what is installed"
        >
          <p className="mt-0.5 break-words text-sm text-muted-foreground">{planError}</p>
        </Banner>
      )}

      {plan !== null && !hasAnything && (
        <Banner
          tone="neutral"
          icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
          title="Nothing installed by this app is left on this computer."
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
            <p className="text-sm font-medium">Will be removed</p>
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
                    Also delete my agent configuration, agents, skills, schedules, and history
                  </span>
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    These are yours, not the app&apos;s: the configuration file holds a working LLM
                    API key, and once deleted it cannot be brought back — reinstalling restores the
                    software, never your files.
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
              <p className="text-sm font-medium">Left alone</p>
              <KeptList items={plan.kept} />
            </div>
          )}

          {plan.manualSteps.length > 0 && (
            <div className="px-4 py-3">
              <p className="text-sm font-medium">You will have to do this yourself</p>
              <ManualStepList steps={plan.manualSteps} />
            </div>
          )}

          <SettingsRow
            label="Remove them"
            description="The local daemon is stopped first. If it will not stop, nothing is deleted."
          >
            <Button
              variant="destructive"
              onClick={remove}
              disabled={running || selected.length === 0}
            >
              {running
                ? 'Removing…'
                : `Remove ${selected.length} ${selected.length === 1 ? 'item' : 'items'} (${formatBytes(totalBytes(selected))})`}
            </Button>
          </SettingsRow>
        </SettingsCard>
      )}

      {runError !== null && (
        <Banner
          tone="error"
          icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
          title="The uninstall did not report back"
        >
          <p className="mt-0.5 break-words text-sm text-muted-foreground">{runError}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Some of it may have been removed and some not — the app never heard the answer. Read the
            list again to see what is still there.
          </p>
        </Banner>
      )}

      {outcome !== null && <OutcomeReport outcome={outcome} />}
    </SettingsSection>
  );
}
