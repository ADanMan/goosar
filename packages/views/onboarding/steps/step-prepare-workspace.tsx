'use client';

import { useEffect, useRef, useState } from 'react';
import { Check, Loader2, TriangleAlert } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { DragStrip } from '@goosar/views/platform';
import { StepHeader } from '../components/step-header';
import type { ProvisioningPackageType, ProvisioningStatus } from '../provisioning-status';
import type { AgentRuntimeStatus } from '../agent-row-status';
import { useT } from '../../i18n';

const ROW_ORDER: readonly ProvisioningPackageType[] = ['skill', 'mcp-server', 'runtime'];

export interface LlmGatewayRowStatus {
  verdict: 'ok' | 'auth_rejected' | 'degraded' | 'unreachable' | 'unconfigured';
  source: 'agent' | 'server';
  host?: string | null;
  standApiBaseHint?: string | null;
}

export interface PxProxyRowStatus {
  state: 'not_required' | 'installed' | 'not_found';
  version?: string;
  mcpCheck?: 'ok' | 'proxy_unreachable' | 'skipped' | { failed: string } | null;
}

function reasonMessage(
  t: ReturnType<typeof useT<'onboarding'>>['t'],
  reasonCode: string | undefined,
): string {
  switch (reasonCode) {
    case 'catalog_unreachable':
      return t(($) => $.step_prepare_workspace.reasons.catalog_unreachable);
    case 'store_unconfigured':
      return t(($) => $.step_prepare_workspace.reasons.store_unconfigured);
    case 'package_install_failed':
      return t(($) => $.step_prepare_workspace.reasons.package_install_failed);
    case 'network_error':
      return t(($) => $.step_prepare_workspace.reasons.network_error);
    default:
      return t(($) => $.step_prepare_workspace.reasons.unknown);
  }
}

export function StepPrepareWorkspace({
  status,
  onRetry,
  onAdvance,
  onBack,
  agentStatus = null,
  onRetryAgent,
  onSkipAgent,
  llmGateway,
  onRetryLlmGateway,
  pxProxy,
}: {
  status: ProvisioningStatus | null;
  onRetry: () => void | Promise<void>;
  onAdvance: () => void;
  onBack?: () => void;
  agentStatus?: AgentRuntimeStatus | null;
  onRetryAgent?: () => void | Promise<void>;
  onSkipAgent?: () => void;
  llmGateway?: LlmGatewayRowStatus | null;
  onRetryLlmGateway?: () => void | Promise<void>;
  pxProxy?: PxProxyRowStatus | null;
}) {
  const { t } = useT('onboarding');

  const [agentSkipped, setAgentSkipped] = useState(false);
  const agentBlocking =
    agentStatus !== null &&
    agentStatus.state !== 'ready' &&
    agentStatus.state !== 'external' &&
    agentStatus.state !== 'unsupported' &&
    !agentSkipped;

  const advancedRef = useRef(false);
  const passThrough = status !== null && status.configured === false;
  const restartPending = status !== null && status.state === 'ok' && status.restartPending === true;
  const ready =
    status !== null && status.state === 'ok' && status.restartPending !== true && !agentBlocking;

  useEffect(() => {
    if (advancedRef.current) return;
    if ((passThrough && !agentBlocking) || ready) {
      advancedRef.current = true;
      onAdvance();
    }
  }, [passThrough, ready, agentBlocking, onAdvance]);

  if (status === null || (passThrough && !agentBlocking) || (ready && !agentBlocking)) {
    return null;
  }

  const failed = status.state === 'fail';

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      <DragStrip />
      <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            {t(($) => $.common.back)}
          </button>
        ) : (
          <span aria-hidden className="w-0" />
        )}
        <div className="flex-1">
          <StepHeader currentStep="workspace" />
        </div>
      </header>

      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
          <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
            {t(($) => $.step_prepare_workspace.headline)}
          </h1>
          <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
            {failed
              ? t(($) => $.step_prepare_workspace.lede_fail)
              : restartPending
                ? t(($) => $.step_prepare_workspace.lede_restart_pending)
                : t(($) => $.step_prepare_workspace.lede_syncing)}
          </p>

          <div className="mt-8 flex flex-col gap-2.5">
            {ROW_ORDER.map((type) => (
              <ProgressRow
                key={type}
                label={
                  type === 'skill'
                    ? t(($) => $.step_prepare_workspace.row_skill)
                    : type === 'mcp-server'
                      ? t(($) => $.step_prepare_workspace.row_mcp_server)
                      : t(($) => $.step_prepare_workspace.row_runtime)
                }
                installed={status.byType[type].installed}
                total={status.byType[type].total}
                failed={failed}
                unavailable={unavailableForType(status.unavailablePackages, type)}
                t={t}
              />
            ))}
            {agentStatus !== null && (
              <AgentRow
                key="agent"
                status={agentStatus}
                skipped={agentSkipped}
                onRetry={onRetryAgent}
                onSkip={
                  onSkipAgent
                    ? () => {
                        setAgentSkipped(true);
                        onSkipAgent();
                      }
                    : undefined
                }
              />
            )}
            {llmGateway && <LlmGatewayRow status={llmGateway} onRetry={onRetryLlmGateway} t={t} />}
            {pxProxy && <PxProxyRow status={pxProxy} t={t} />}
          </div>

          {failed && (
            <p
              aria-live="polite"
              className="mt-6 flex items-start gap-1.5 text-[13px] leading-[1.55] text-destructive"
            >
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {reasonMessage(t, status.reasonCode)}
            </p>
          )}

          <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
              {failed
                ? t(($) => $.step_prepare_workspace.hint_fail)
                : restartPending
                  ? t(($) => $.step_prepare_workspace.hint_restart_pending)
                  : t(($) => $.step_prepare_workspace.hint_syncing)}
            </span>
            {failed ? (
              <Button size="lg" onClick={() => void onRetry()}>
                {t(($) => $.step_prepare_workspace.retry)}
              </Button>
            ) : (
              <Button size="lg" variant="secondary" onClick={onAdvance}>
                {t(($) => $.step_prepare_workspace.skip)}
              </Button>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

function LlmGatewayRow({
  status,
  onRetry,
  t,
}: {
  status: LlmGatewayRowStatus;
  onRetry?: () => void | Promise<void>;
  t: ReturnType<typeof useT<'onboarding'>>['t'];
}) {
  const { verdict, source, host, standApiBaseHint } = status;
  const ok = verdict === 'ok';
  const tone: 'success' | 'destructive' | 'warning' | 'neutral' =
    verdict === 'ok'
      ? 'success'
      : verdict === 'auth_rejected' || verdict === 'unreachable'
        ? 'destructive'
        : verdict === 'degraded'
          ? 'warning'
          : 'neutral';
  const verdictLabel =
    host && (verdict === 'unreachable' || verdict === 'auth_rejected')
      ? t(($) => $.step_prepare_workspace[`ai_gateway_${verdict}_host`], { host })
      : t(($) => $.step_prepare_workspace[`ai_gateway_${verdict}`]);

  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3">
      <span
        aria-hidden
        className={cn(
          'flex h-6 w-6 shrink-0 items-center justify-center rounded-full',
          tone === 'success'
            ? 'bg-success/15 text-success'
            : tone === 'destructive'
              ? 'bg-destructive/10 text-destructive'
              : tone === 'warning'
                ? 'bg-warning/15 text-warning'
                : 'bg-muted text-muted-foreground',
        )}
      >
        {ok ? <Check className="h-3.5 w-3.5" /> : <TriangleAlert className="h-3.5 w-3.5" />}
      </span>
      <span className="flex-1 text-[14px] text-foreground">
        {t(($) => $.step_prepare_workspace.row_ai_gateway)}
        {source === 'server' && (
          <span className="ml-1.5 text-[12px] text-muted-foreground">
            ({t(($) => $.step_prepare_workspace.ai_gateway_source_server)})
          </span>
        )}
      </span>
      <span
        className={cn(
          'text-[12.5px]',
          tone === 'success'
            ? 'text-success'
            : tone === 'destructive'
              ? 'text-destructive'
              : tone === 'warning'
                ? 'text-warning'
                : 'text-muted-foreground',
        )}
      >
        {verdictLabel}
        {standApiBaseHint && (
          <>
            {' · '}
            {t(($) => $.step_prepare_workspace.ai_gateway_switch_hint, {
              host: standApiBaseHint,
            })}
          </>
        )}
      </span>
      {!ok && onRetry && (
        <Button size="sm" variant="ghost" onClick={() => void onRetry()}>
          {t(($) => $.step_prepare_workspace.ai_gateway_retry)}
        </Button>
      )}
    </div>
  );
}

function PxProxyRow({
  status,
  t,
}: {
  status: PxProxyRowStatus;
  t: ReturnType<typeof useT<'onboarding'>>['t'];
}) {
  const { state, version, mcpCheck } = status;
  const ok = state !== 'not_found';
  const tone: 'success' | 'warning' | 'neutral' =
    state === 'installed' ? 'success' : state === 'not_found' ? 'warning' : 'neutral';
  const stateLabel =
    state === 'not_required'
      ? t(($) => $.step_prepare_workspace.px_not_required)
      : state === 'installed'
        ? version
          ? t(($) => $.step_prepare_workspace.px_installed_version, { version })
          : t(($) => $.step_prepare_workspace.px_installed)
        : t(($) => $.step_prepare_workspace.px_not_found);

  const mcpCheckLabel =
    mcpCheck && mcpCheck !== 'skipped'
      ? mcpCheck === 'ok'
        ? t(($) => $.step_prepare_workspace.px_mcp_ok)
        : mcpCheck === 'proxy_unreachable'
          ? t(($) => $.step_prepare_workspace.px_mcp_proxy_unreachable)
          : t(($) => $.step_prepare_workspace.px_mcp_failed, {
              reason: mcpCheck.failed,
            })
      : null;

  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3">
      <span
        aria-hidden
        className={cn(
          'flex h-6 w-6 shrink-0 items-center justify-center rounded-full',
          tone === 'success'
            ? 'bg-success/15 text-success'
            : tone === 'warning'
              ? 'bg-warning/15 text-warning'
              : 'bg-muted text-muted-foreground',
        )}
      >
        {ok ? <Check className="h-3.5 w-3.5" /> : <TriangleAlert className="h-3.5 w-3.5" />}
      </span>
      <span className="flex-1 text-[14px] text-foreground">
        {t(($) => $.step_prepare_workspace.row_px_proxy)}
        {mcpCheckLabel && (
          <span className="ml-1.5 text-[12px] text-muted-foreground">
            ({t(($) => $.step_prepare_workspace.px_mcp_check_label)}: {mcpCheckLabel})
          </span>
        )}
      </span>
      <span
        className={cn(
          'text-[12.5px]',
          tone === 'success'
            ? 'text-success'
            : tone === 'warning'
              ? 'text-warning'
              : 'text-muted-foreground',
        )}
      >
        {stateLabel}
      </span>
    </div>
  );
}

function unavailableForType(
  entries: { key: string; reason: string }[] | undefined,
  type: ProvisioningPackageType,
): { name: string; reason: string }[] {
  if (!entries || entries.length === 0) return [];
  const prefix = `${type}:`;
  return entries
    .filter((e) => e.key.startsWith(prefix))
    .map((e) => {
      const rest = e.key.slice(prefix.length);
      const at = rest.indexOf('@');
      return { name: at >= 0 ? rest.slice(0, at) : rest, reason: e.reason };
    });
}

function ProgressRow({
  label,
  installed,
  total,
  failed,
  unavailable = [],
  t,
}: {
  label: string;
  installed: number;
  total: number;
  failed: boolean;
  unavailable?: { name: string; reason: string }[];
  t: ReturnType<typeof useT<'onboarding'>>['t'];
}) {
  const done = total === 0 || installed >= total;
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3">
      <span
        aria-hidden
        className={cn(
          'flex h-6 w-6 shrink-0 items-center justify-center rounded-full',
          done
            ? 'bg-success/15 text-success'
            : failed
              ? 'bg-destructive/10 text-destructive'
              : 'bg-muted text-muted-foreground',
        )}
      >
        {done ? (
          <Check className="h-3.5 w-3.5" />
        ) : failed ? (
          <TriangleAlert className="h-3.5 w-3.5" />
        ) : (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        )}
      </span>
      <span className="flex-1 text-[14px] text-foreground">{label}</span>
      <span
        className="font-mono text-[12.5px] text-muted-foreground"
        title={
          unavailable.length > 0
            ? unavailable.map((u) => `${u.name}: ${u.reason}`).join('\n')
            : undefined
        }
      >
        {installed}/{total}
        {unavailable.length > 0 &&
          t(($) => $.step_prepare_workspace.row_unavailable, {
            count: unavailable.length,
            names: unavailable.map((u) => u.name).join(', '),
          })}
      </span>
    </div>
  );
}

function AgentRow({
  status,
  skipped,
  onRetry,
  onSkip,
}: {
  status: AgentRuntimeStatus;
  skipped: boolean;
  onRetry?: () => void | Promise<void>;
  onSkip?: () => void;
}) {
  const { t } = useT('onboarding');

  const isReady = status.state === 'ready';
  const needsConfig = status.state === 'needs_config';
  const isError = status.state === 'not_installed' || status.state === 'unsupported';
  const isWorking = status.state === 'checking' || status.state === 'installing';

  const label = t(($) => $.step_prepare_workspace.agent_row.label);
  const statusText = isReady
    ? t(($) => $.step_prepare_workspace.agent_row.ready, {
        version: status.version ?? '',
      })
    : needsConfig
      ? t(($) => $.step_prepare_workspace.agent_row.needs_config)
      : isError
        ? t(($) => $.step_prepare_workspace.agent_row.error)
        : status.state === 'installing'
          ? t(($) => $.step_prepare_workspace.agent_row.installing)
          : t(($) => $.step_prepare_workspace.agent_row.waiting);

  const detailLower = status.detail?.toLowerCase() ?? '';
  const reasonText =
    status.state === 'unsupported'
      ? t(($) => $.step_prepare_workspace.agent_row.reasons.windows_unsupported)
      : detailLower.includes('timed out') || detailLower.includes('timeout')
        ? t(($) => $.step_prepare_workspace.agent_row.reasons.timeout)
        : detailLower.includes('did not run')
          ? t(($) => $.step_prepare_workspace.agent_row.reasons.runtime_not_starting)
          : (status.detail ?? null);

  return (
    <div className="flex flex-col gap-1.5 rounded-lg border bg-card px-4 py-3">
      <div className="flex items-center gap-3">
        <span
          aria-hidden
          className={cn(
            'flex h-6 w-6 shrink-0 items-center justify-center rounded-full',
            isReady
              ? 'bg-success/15 text-success'
              : isError && !skipped
                ? 'bg-destructive/10 text-destructive'
                : needsConfig && !skipped
                  ? 'bg-warning/15 text-warning'
                  : 'bg-muted text-muted-foreground',
          )}
        >
          {isReady ? (
            <Check className="h-3.5 w-3.5" />
          ) : (isError || needsConfig) && !skipped ? (
            <TriangleAlert className="h-3.5 w-3.5" />
          ) : (
            <Loader2 className={cn('h-3.5 w-3.5', isWorking && 'animate-spin')} />
          )}
        </span>
        <span className="flex-1 text-[14px] text-foreground">{label}</span>
        <span className="text-[12.5px] text-muted-foreground">{statusText}</span>
        {(isError || needsConfig) && !skipped && onRetry && (
          <Button size="sm" variant="secondary" onClick={() => void onRetry()}>
            {t(($) => $.step_prepare_workspace.agent_row.retry)}
          </Button>
        )}
      </div>
      {isReady && status.bashProbe != null && (
        <p className="pl-9 text-[12px] text-muted-foreground">
          {status.bashProbe.ok
            ? t(($) => $.step_prepare_workspace.agent_row.bash_ok)
            : t(($) => $.step_prepare_workspace.agent_row.bash_failed, {
                reason: status.bashProbe.reason ?? '',
              })}
        </p>
      )}
      {isError && !skipped && (
        <div className="flex items-center justify-between gap-2 pl-9">
          {reasonText ? <p className="text-[12px] text-destructive">{reasonText}</p> : <span />}
          {onSkip && status.state !== 'unsupported' && (
            <Button size="sm" variant="ghost" onClick={onSkip}>
              {t(($) => $.step_prepare_workspace.agent_row.continue_without)}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
