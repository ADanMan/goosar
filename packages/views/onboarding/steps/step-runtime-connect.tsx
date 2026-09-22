'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, Loader2, Monitor, RefreshCw } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { runtimeKeys } from '@goosar/core/runtimes/queries';
import { effectiveConfigOptions } from '@goosar/core/workspace/effective-config';
import {
  ONBOARDING_HERMES_ONLY,
  runtimeDisplayLabel,
  runtimeDisplayName,
} from '@goosar/core/runtimes';
import type { AgentRuntime } from '@goosar/core/types';
import { DragStrip } from '@goosar/views/platform';
import { StepHeader } from '../components/step-header';
import { RuntimeAsidePanel } from '../components/runtime-aside-panel';
import { useRuntimePicker } from '../components/use-runtime-picker';
import {
  LlmConnectionForm,
  type LlmExistingConnectionInfo,
  type LlmServerConnectionInfo,
  type SaveLlmConnection,
} from '../components/llm-connection-form';
import type { PerimeterMachineState } from '../perimeter-machine';
import { ProviderLogo } from '../../runtimes/components/provider-logo';
import {
  buildRuntimeMachines,
  type RuntimeMachine,
} from '../../runtimes/components/runtime-machines';
import { useT } from '../../i18n';

export function StepRuntimeConnect({
  wsId,
  onNext,
  onBack,
  onRefresh,
  runtimesPending,
  onSaveLlmConnection,
  localDaemonId,
  localMachineName,
  perimeterMachine,
  existingLlmConnection,
  daemonState,
  networkStatusSlot,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  onRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
  onSaveLlmConnection?: SaveLlmConnection;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  perimeterMachine?: PerimeterMachineState;
  existingLlmConnection?: LlmExistingConnectionInfo | null;
  daemonState?: string | null;
  networkStatusSlot?: ReactNode;
}) {
  const { runtimes, selected, selectedId, setSelectedId } = useRuntimePicker(wsId);

  return (
    <FancyView
      wsId={wsId}
      runtimes={runtimes}
      selected={selected}
      selectedId={selectedId}
      setSelectedId={setSelectedId}
      onNext={onNext}
      onBack={onBack}
      onRefresh={onRefresh}
      runtimesPending={runtimesPending}
      onSaveLlmConnection={onSaveLlmConnection}
      localDaemonId={localDaemonId}
      localMachineName={localMachineName}
      perimeterMachine={perimeterMachine}
      existingLlmConnection={existingLlmConnection}
      daemonState={daemonState}
      networkStatusSlot={networkStatusSlot}
    />
  );
}

type Phase = 'scanning' | 'found' | 'empty';

const EMPTY_TIMEOUT_MS = 5000;

const EMPTY_HARD_TIMEOUT_MS = 20000;

function FancyView({
  wsId,
  runtimes,
  selected,
  selectedId,
  setSelectedId,
  onNext,
  onBack,
  onRefresh,
  runtimesPending,
  onSaveLlmConnection,
  localDaemonId,
  localMachineName,
  perimeterMachine,
  existingLlmConnection,
  daemonState,
  networkStatusSlot,
}: {
  wsId: string;
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  onRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
  onSaveLlmConnection?: SaveLlmConnection;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  perimeterMachine?: PerimeterMachineState;
  existingLlmConnection?: LlmExistingConnectionInfo | null;
  daemonState?: string | null;
  networkStatusSlot?: ReactNode;
}) {
  const { t } = useT('onboarding');
  const qc = useQueryClient();
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const { data: effectiveConfig } = useQuery(effectiveConfigOptions(wsId));
  const serverLlm = useMemo<LlmServerConnectionInfo | null>(() => {
    const llm = effectiveConfig?.llm;
    if (!llm || llm.origin === '') return null;
    return {
      origin: llm.origin,
      locked: llm.locked === true,
      hasApiKey: llm.has_api_key === true,
      baseUrl: llm.base_url,
      model: llm.model,
    };
  }, [effectiveConfig]);

  const [scanEpoch, setScanEpoch] = useState(0);
  const [softTimedOut, setSoftTimedOut] = useState(false);
  const [hardTimedOut, setHardTimedOut] = useState(false);
  useEffect(() => {
    if (runtimes.length > 0) return;
    setSoftTimedOut(false);
    setHardTimedOut(false);
    const soft = window.setTimeout(() => setSoftTimedOut(true), EMPTY_TIMEOUT_MS);
    const hard = window.setTimeout(() => setHardTimedOut(true), EMPTY_HARD_TIMEOUT_MS);
    return () => {
      window.clearTimeout(soft);
      window.clearTimeout(hard);
    };
  }, [runtimes.length, scanEpoch]);

  const phase: Phase =
    runtimes.length > 0
      ? 'found'
      : hardTimedOut || (softTimedOut && runtimesPending !== true)
        ? 'empty'
        : 'scanning';

  const onlineCount = runtimes.filter((r) => r.status === 'online').length;

  const machines = useMemo<RuntimeMachine[]>(
    () =>
      ONBOARDING_HERMES_ONLY
        ? buildRuntimeMachines(runtimes, {
            now: Date.now(),
            localDaemonId,
            localMachineName,
          })
        : [],
    [runtimes, localDaemonId, localMachineName],
  );

  const [submitting, setSubmitting] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const handleRefresh = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      if (onRefresh) await onRefresh();
      await qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      setScanEpoch((n) => n + 1);
    } finally {
      setRefreshing(false);
    }
  }, [onRefresh, qc, wsId, refreshing]);

  const handleSkip = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onNext(null);
    } finally {
      setSubmitting(false);
    }
  };
  const canContinue = phase === 'found' && selected !== null;
  const handleContinue = async () => {
    if (!canContinue || submitting) return;
    setSubmitting(true);
    try {
      await onNext(selected);
    } finally {
      setSubmitting(false);
    }
  };

  const llmForm =
    selected?.provider === 'runtime-j' && onSaveLlmConnection ? (
      <LlmConnectionForm
        onSave={onSaveLlmConnection}
        className="mt-6"
        perimeterCaMissing={perimeterMachine?.caBundleMissing === true}
        existingConnection={existingLlmConnection}
        serverConnection={serverLlm}
      />
    ) : null;

  const footerHint =
    phase === 'found' && selected
      ? t(($) => $.step_runtime.hint_selected, {
          name: runtimeDisplayLabel(selected),
        })
      : phase === 'found'
        ? ONBOARDING_HERMES_ONLY
          ? t(($) => $.step_runtime.hint_pick_machine)
          : t(($) => $.step_runtime.hint_pick)
        : phase === 'scanning'
          ? t(($) => $.step_runtime.hint_waiting)
          : ONBOARDING_HERMES_ONLY
            ? t(($) => $.step_runtime.hint_skip_or_refresh_hermes)
            : t(($) => $.step_runtime.hint_skip_or_refresh);

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />

        {/* Header — Back + horizontal step indicator */}
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t(($) => $.common.back)}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="runtime" />
          </div>
        </header>

        {/* Scrollable middle — content changes by phase but always wraps
            at max-w-[620px] so the 2-column runtime grid has room to
            breathe without stretching into readability territory.

            Skip + Continue sit inline directly below the phase view
            (not in a sticky bottom footer) so the action bar stays
            close to the form content and the page doesn't leave a
            large dead zone when the runtime list is short. */}
        <main ref={mainRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto">
          {/* key=phase forces a remount on phase transition so the
              `animate-onboarding-enter` animation replays — otherwise CSS
              only runs on initial mount and scanning→found would be a
              hard cut. */}
          <div
            key={phase}
            className="animate-onboarding-enter mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14"
          >
            {/* Hermes-only mode swaps each phase view for its machine-focused
                counterpart; the multi-provider views stay reachable behind the
                flag so switching it off restores the provider picker. */}
            {phase === 'scanning' &&
              (ONBOARDING_HERMES_ONLY ? <HermesScanningView /> : <ScanningView />)}
            {phase === 'found' &&
              (ONBOARDING_HERMES_ONLY ? (
                <MachineFoundView
                  machines={machines}
                  selectedId={selectedId}
                  onSelect={setSelectedId}
                  onRefresh={handleRefresh}
                  refreshing={refreshing}
                  llmForm={llmForm}
                />
              ) : (
                <FoundView
                  runtimes={runtimes}
                  selectedId={selectedId}
                  onSelect={setSelectedId}
                  onlineCount={onlineCount}
                  onRefresh={handleRefresh}
                  refreshing={refreshing}
                  llmForm={llmForm}
                />
              ))}
            {phase === 'empty' &&
              (ONBOARDING_HERMES_ONLY ? (
                <HermesEmptyView
                  onSkip={handleSkip}
                  onRefresh={handleRefresh}
                  refreshing={refreshing}
                  daemonState={daemonState}
                  networkStatusSlot={networkStatusSlot}
                />
              ) : (
                <EmptyView onSkip={handleSkip} onRefresh={handleRefresh} refreshing={refreshing} />
              ))}

            {/* Footer action bar. The controls are phase-scoped so no dead or
                duplicated affordance ever shows:
                  - Skip: shown while scanning / found. The empty phase owns its
                    own prominent Skip card, so the footer Skip is dropped there
                    to avoid two "Skip for now" buttons on one screen.
                  - Start exploring: only actionable once a runtime is picked, so
                    it renders only in the found phase instead of sitting
                    permanently disabled through scanning / empty. */}
            <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
              <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
                {footerHint}
              </span>
              {phase !== 'empty' && (
                <div className="flex items-center gap-2">
                  <Button size="lg" variant="secondary" disabled={submitting} onClick={handleSkip}>
                    {t(($) => $.step_runtime.skip)}
                  </Button>
                  {phase === 'found' && (
                    <Button
                      size="lg"
                      disabled={!canContinue || submitting}
                      onClick={handleContinue}
                    >
                      {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                      {t(($) => $.step_runtime.start_exploring)}
                      <ArrowRight className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              )}
            </div>
          </div>
        </main>
      </div>

      {/* Right — always-visible educational aside. "You picked" subsection
          only appears when there's a selection; the other two stay constant. */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <RuntimeAsidePanel />
        </div>
      </aside>
    </div>
  );
}

function ScanningView() {
  const { t } = useT('onboarding');
  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        {t(($) => $.step_runtime.scanning_headline)}
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.scanning_lede_prefix)}
        <span className="font-medium text-foreground">{'Runtime C'}</span>
        {', '}
        <span className="font-medium text-foreground">{'Runtime E'}</span>
        {', '}
        <span className="font-medium text-foreground">{'Runtime G'}</span>
        {t(($) => $.step_runtime.scanning_lede_suffix)}
      </p>
      <div className="mt-10 grid grid-cols-1 gap-2.5 sm:grid-cols-2">
        <SkeletonRuntimeCard />
        <SkeletonRuntimeCard />
      </div>
    </div>
  );
}

function HermesScanningView() {
  const { t } = useT('onboarding');
  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        {t(($) => $.step_runtime.scanning_headline)}
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.scanning_lede_hermes_prefix)}
        <span className="font-medium text-foreground">{'hermes'}</span>
        {t(($) => $.step_runtime.scanning_lede_hermes_suffix)}
      </p>
      <div className="mt-10 flex flex-col gap-2.5">
        <SkeletonRuntimeCard />
      </div>
    </div>
  );
}

function MachineFoundView({
  machines,
  selectedId,
  onSelect,
  onRefresh,
  refreshing,
  llmForm,
}: {
  machines: RuntimeMachine[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
  refreshing: boolean;
  llmForm?: ReactNode;
}) {
  const { t } = useT('onboarding');
  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
          {t(($) => $.step_runtime.found_headline_hermes)}
        </h1>
        <RefreshButton onClick={onRefresh} refreshing={refreshing} className="mt-2 shrink-0" />
      </div>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.found_lede_hermes)}
      </p>

      <div
        role="radiogroup"
        aria-label={t(($) => $.step_runtime.machine_group_label)}
        className="mt-8 flex flex-col gap-2.5"
      >
        {machines.map((machine) => {
          const selected = machine.runtimes.some((r) => r.id === selectedId);
          return (
            <MachineCard
              key={machine.id}
              machine={machine}
              selected={selected}
              onSelect={() => {
                if (selected) return;
                const first = machine.runtimes[0];
                if (first) onSelect(first.id);
              }}
            />
          );
        })}
      </div>

      {llmForm}
    </div>
  );
}

function MachineCard({
  machine,
  selected,
  onSelect,
}: {
  machine: RuntimeMachine;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT('onboarding');
  const online = machine.onlineCount > 0;
  const title = machine.isCurrent ? t(($) => $.step_runtime.machine_this_computer) : machine.title;
  const detail = machine.isCurrent ? machine.title : machine.subtitle;

  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        'flex items-center gap-3 rounded-lg border bg-card p-4 text-left transition-colors',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20',
      )}
    >
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-accent/30">
        <Monitor className="h-4 w-4" aria-hidden />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-foreground">{title}</div>
        <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
          <span
            className={cn(
              'h-1.5 w-1.5 shrink-0 rounded-full',
              online ? 'bg-success' : 'bg-muted-foreground/40',
            )}
            aria-hidden
          />
          {online ? t(($) => $.step_runtime.online_label) : t(($) => $.step_runtime.offline_label)}
          {detail && detail !== title && (
            <>
              <span aria-hidden>{'·'}</span>
              <span className="truncate">{detail}</span>
            </>
          )}
        </div>
      </div>
      <RadioMark selected={selected} />
    </button>
  );
}

function HermesEmptyView({
  onSkip,
  onRefresh,
  refreshing,
  daemonState,
  networkStatusSlot,
}: {
  onSkip: () => void;
  onRefresh: () => void;
  refreshing: boolean;
  daemonState?: string | null;
  networkStatusSlot?: ReactNode;
}) {
  const { t } = useT('onboarding');

  const daemonReason = ((): string | null => {
    switch (daemonState) {
      case null:
      case undefined:
        return null;
      case 'running':
        return t(($) => $.step_runtime.empty_daemon_running);
      case 'stopped':
        return t(($) => $.step_runtime.empty_daemon_stopped);
      case 'starting':
        return t(($) => $.step_runtime.empty_daemon_starting);
      case 'stopping':
        return t(($) => $.step_runtime.empty_daemon_stopping);
      case 'installing_cli':
        return t(($) => $.step_runtime.empty_daemon_installing_cli);
      case 'cli_not_found':
        return t(($) => $.step_runtime.empty_daemon_cli_not_found);
      case 'auth_expired':
        return t(($) => $.step_runtime.empty_daemon_auth_expired);
      default:
        return t(($) => $.step_runtime.empty_daemon_unknown);
    }
  })();

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
          {t(($) => $.step_runtime.empty_headline_hermes)}
        </h1>
        <RefreshButton onClick={onRefresh} refreshing={refreshing} className="mt-2 shrink-0" />
      </div>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.empty_lede_hermes_prefix)}
        <span className="font-medium text-foreground">{'hermes'}</span>
        {t(($) => $.step_runtime.empty_lede_hermes_suffix)}
      </p>

      {daemonReason !== null && (
        <p
          data-testid="empty-daemon-reason"
          className="mt-4 max-w-[560px] rounded-md border bg-muted/40 px-3 py-2.5 text-[13px] leading-[1.55] text-muted-foreground"
        >
          {daemonReason}
        </p>
      )}

      {networkStatusSlot !== undefined && networkStatusSlot !== null && (
        <div className="mt-3 flex flex-wrap items-center gap-2 text-[13px] text-muted-foreground">
          <span>{t(($) => $.step_runtime.empty_network_status_hint)}</span>
          {networkStatusSlot}
        </div>
      )}

      <div className="mt-10 flex flex-col gap-3.5">
        <EmptyCard
          title={t(($) => $.step_runtime.empty_skip_title)}
          subtitle={t(($) => $.step_runtime.empty_skip_subtitle)}
          actionLabel={t(($) => $.step_runtime.empty_skip_action)}
          onAction={onSkip}
        />

        <ComingSoonCard
          title={t(($) => $.step_runtime.empty_waitlist_title)}
          subtitle={t(($) => $.step_runtime.empty_waitlist_subtitle)}
          badgeLabel={t(($) => $.step_runtime.empty_waitlist_action)}
        />
      </div>
    </div>
  );
}

function FoundView({
  runtimes,
  selectedId,
  onSelect,
  onlineCount,
  onRefresh,
  refreshing,
  llmForm,
}: {
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onlineCount: number;
  onRefresh: () => void;
  refreshing: boolean;
  llmForm?: ReactNode;
}) {
  const { t } = useT('onboarding');
  const total = runtimes.length;
  const statusLabel =
    onlineCount === total
      ? t(($) => $.step_runtime.status_all_online)
      : onlineCount === 0
        ? t(($) => $.step_runtime.status_none_online)
        : t(($) => $.step_runtime.status_n_online, { count: onlineCount });
  const statusTone = onlineCount === 0 ? 'text-muted-foreground' : 'text-success';

  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        {t(($) => $.step_runtime.found_headline)}
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.found_lede)}
      </p>

      <div className="mt-8 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-muted/60 px-4 py-2.5 text-xs">
        <span className="font-semibold text-foreground">
          {t(($) => $.step_runtime.runtime_count, { count: total })}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className={cn('flex items-center gap-1', statusTone)}>
          <span
            className={cn(
              'h-1.5 w-1.5 rounded-full',
              onlineCount === 0 ? 'bg-muted-foreground/40' : 'bg-success',
            )}
            aria-hidden
          />
          {statusLabel}
        </span>
        <RefreshButton onClick={onRefresh} refreshing={refreshing} className="ml-auto" />
      </div>

      <div
        role="radiogroup"
        aria-label={t(($) => $.step_runtime.runtime_group_label)}
        className="mt-6 grid grid-cols-1 gap-2.5 sm:grid-cols-2"
      >
        {runtimes.map((rt) => (
          <RuntimeCard
            key={rt.id}
            runtime={rt}
            selected={rt.id === selectedId}
            onSelect={() => onSelect(rt.id)}
          />
        ))}
      </div>

      {llmForm}
    </div>
  );
}

function EmptyView({
  onSkip,
  onRefresh,
  refreshing,
}: {
  onSkip: () => void;
  onRefresh: () => void;
  refreshing: boolean;
}) {
  const { t } = useT('onboarding');

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
          {t(($) => $.step_runtime.empty_headline)}
        </h1>
        <RefreshButton onClick={onRefresh} refreshing={refreshing} className="mt-2 shrink-0" />
      </div>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.empty_lede_prefix)}
        <span className="font-medium text-foreground">{'Runtime C'}</span>
        {', '}
        <span className="font-medium text-foreground">{'Runtime E'}</span>
        {', '}
        <span className="font-medium text-foreground">{'Runtime G'}</span>
        {t(($) => $.step_runtime.empty_lede_suffix)}
      </p>

      <div className="mt-10 flex flex-col gap-3.5">
        <EmptyCard
          title={t(($) => $.step_runtime.empty_skip_title)}
          subtitle={t(($) => $.step_runtime.empty_skip_subtitle)}
          actionLabel={t(($) => $.step_runtime.empty_skip_action)}
          onAction={onSkip}
        />

        <ComingSoonCard
          title={t(($) => $.step_runtime.empty_waitlist_title)}
          subtitle={t(($) => $.step_runtime.empty_waitlist_subtitle)}
          badgeLabel={t(($) => $.step_runtime.empty_waitlist_action)}
        />
      </div>
    </div>
  );
}

function ComingSoonCard({
  title,
  subtitle,
  badgeLabel,
}: {
  title: string;
  subtitle: string;
  badgeLabel: string;
}) {
  return (
    <div
      aria-disabled
      className="flex items-center justify-between gap-4 rounded-lg border border-dashed bg-muted/20 px-5 py-4 opacity-70"
    >
      <div className="min-w-0">
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">{subtitle}</p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center rounded-full border bg-background px-3 py-1.5 text-[12px] font-medium uppercase tracking-wide text-muted-foreground"
      >
        {badgeLabel}
      </span>
    </div>
  );
}

function RefreshButton({
  onClick,
  refreshing,
  className,
}: {
  onClick: () => void;
  refreshing: boolean;
  className?: string;
}) {
  const { t } = useT('onboarding');
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={refreshing}
      onClick={onClick}
      className={className}
    >
      <RefreshCw className={cn('h-3.5 w-3.5', refreshing && 'animate-spin')} aria-hidden />
      {refreshing ? t(($) => $.step_runtime.refreshing) : t(($) => $.step_runtime.refresh)}
    </Button>
  );
}

function EmptyCard({
  title,
  subtitle,
  actionLabel,
  onAction,
}: {
  title: string;
  subtitle: string;
  actionLabel: string;
  onAction: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onAction}
      className="group flex items-center justify-between gap-4 rounded-lg border bg-card px-5 py-4 text-left transition-colors hover:border-foreground/30 hover:bg-muted/30"
    >
      <div className="min-w-0">
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">{subtitle}</p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full border bg-background px-4 py-2 text-[13px] font-medium text-foreground transition-colors group-hover:border-foreground group-hover:bg-foreground group-hover:text-background"
      >
        {actionLabel}
        <ArrowRight className="h-3.5 w-3.5" />
      </span>
    </button>
  );
}

function RuntimeCard({
  runtime,
  selected,
  onSelect,
}: {
  runtime: AgentRuntime;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT('onboarding');
  const online = runtime.status === 'online';

  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        'flex items-center gap-3 rounded-lg border bg-card p-4 text-left transition-colors',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20',
      )}
    >
      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent/30">
        <ProviderLogo provider={runtime.provider} className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-foreground">
          {runtimeDisplayName(runtime)}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
          <span
            className={cn(
              'h-1.5 w-1.5 rounded-full',
              online ? 'bg-success' : 'bg-muted-foreground/40',
            )}
            aria-hidden
          />
          {online ? t(($) => $.step_runtime.online_label) : t(($) => $.step_runtime.offline_label)}
        </div>
      </div>
      <RadioMark selected={selected} />
    </button>
  );
}

function SkeletonRuntimeCard() {
  return (
    <div
      aria-hidden
      className="flex animate-pulse items-center gap-3 rounded-lg border bg-card p-4"
    >
      <div className="h-7 w-7 shrink-0 rounded-md bg-muted" />
      <div className="flex-1 space-y-2">
        <div className="h-3 w-28 rounded bg-muted" />
        <div className="h-2.5 w-16 rounded bg-muted/70" />
      </div>
      <div className="h-4 w-4 shrink-0 rounded-full border-[1.5px] border-muted" />
    </div>
  );
}

function RadioMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        'relative inline-block h-4 w-4 shrink-0 rounded-full border-[1.5px] transition-colors',
        selected ? 'border-foreground' : 'border-border',
      )}
    >
      {selected && <span className="absolute inset-[3px] rounded-full bg-foreground" />}
    </span>
  );
}
