import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Check,
  CircleHelp,
  KeyRound,
  Loader2,
  RefreshCw,
  Shield,
  ShieldAlert,
  ShieldCheck,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { Popover, PopoverContent, PopoverTrigger } from '@goosar/ui/components/ui/popover';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { cn } from '@goosar/ui/lib/utils';
import { useT, useUiLocale } from '@goosar/views/i18n';
import {
  type PerimeterCheck,
  type PerimeterCheckId,
  type PerimeterCheckState,
  type PerimeterStateView,
  type TicketSource,
} from '../../../shared/perimeter-config';
import { redactAddressCredentials } from '../../../shared/system-proxy';
import { DoctorSection } from './doctor-report';
import { kinitFailureMessage } from './kinit-copy';
import {
  saveKerberosPrincipalOverride,
  useKerberosPreferences,
  useResolvedKerberosPrincipal,
} from './use-kerberos-principal';
import {
  decideKerberosAction,
  KERBEROS_SNOOZE_MS,
  validateKerberosPrincipalInput,
} from '../../../shared/kerberos-identity';

type PerimeterT = ReturnType<typeof useT<'settings'>>['t'];

function checkStateToneClass(state: PerimeterCheckState): string {
  switch (state) {
    case 'ok':
      return 'text-emerald-500';
    case 'degraded':
      return 'text-warning';
    case 'fail':
      return 'text-destructive';
    case 'unknown':
      return 'text-muted-foreground';
  }
}

function CheckStateIcon({ state }: { state: PerimeterCheckState }) {
  const className = cn('mt-0.5 size-3.5 shrink-0', checkStateToneClass(state));
  switch (state) {
    case 'ok':
      return <Check className={className} />;
    case 'degraded':
      return <ShieldAlert className={className} />;
    case 'fail':
      return <X className={className} />;
    case 'unknown':
      return <CircleHelp className={className} />;
  }
}

function liveItemLabel(t: PerimeterT, id: PerimeterCheckId): string {
  switch (id) {
    case 'route_server':
      return t(($) => $.desktop.perimeter.live_item_route_server);
    case 'route_llm':
      return t(($) => $.desktop.perimeter.live_item_route_llm);
    case 'entry':
      return t(($) => $.desktop.perimeter.live_item_entry);
    case 'ca':
      return t(($) => $.desktop.perimeter.live_item_ca);
    case 'px':
      return t(($) => $.desktop.perimeter.live_item_px);
    case 'kerberos':
      return t(($) => $.desktop.perimeter.live_item_kerberos);
    case 'server':
      return t(($) => $.desktop.perimeter.live_item_server);
    case 'llm':
      return t(($) => $.desktop.perimeter.live_item_llm);
    case 'daemon':
      return t(($) => $.desktop.perimeter.live_item_daemon);
    case 'provisioning':
      return t(($) => $.desktop.perimeter.live_item_provisioning);
  }
}

function liveReasonLabel(t: PerimeterT, reasonCode: string): string {
  switch (reasonCode) {
    case 'route_proxy':
      return t(($) => $.desktop.perimeter.route_proxy);
    case 'route_proxy_list':
      return t(($) => $.desktop.perimeter.route_proxy_list);
    case 'route_direct':
      return t(($) => $.desktop.perimeter.route_direct);
    case 'route_unknown':
      return t(($) => $.desktop.perimeter.route_unknown);
    case 'route_unconfigured':
      return t(($) => $.desktop.perimeter.route_unconfigured);
    case 'entry_local_proxy':
      return t(($) => $.desktop.perimeter.entry_local_proxy);
    case 'entry_system_proxy':
      return t(($) => $.desktop.perimeter.entry_system_proxy);
    case 'entry_direct':
      return t(($) => $.desktop.perimeter.entry_direct);
    case 'entry_unknown':
      return t(($) => $.desktop.perimeter.entry_unknown);
    case 'entry_conflict':
      return t(($) => $.desktop.perimeter.entry_conflict);
    case 'ca_ok':
      return t(($) => $.desktop.perimeter.ca_ok);
    case 'ca_absent':
      return t(($) => $.desktop.perimeter.ca_absent);
    case 'live_px_ok':
      return t(($) => $.desktop.perimeter.live_px_ok);
    case 'live_px_unreachable':
      return t(($) => $.desktop.perimeter.live_px_unreachable);
    case 'live_px_unconfigured':
      return t(($) => $.desktop.perimeter.live_px_unconfigured);
    case 'live_px_not_loopback':
      return t(($) => $.desktop.perimeter.live_px_not_loopback);
    case 'live_px_upstream_dead':
      return t(($) => $.desktop.perimeter.live_px_upstream_dead);
    case 'live_px_executable_missing':
      return t(($) => $.desktop.perimeter.live_px_executable_missing);
    case 'ticket_valid':
      return t(($) => $.desktop.perimeter.ticket_valid);
    case 'ticket_not_accepted':
      return t(($) => $.desktop.perimeter.ticket_not_accepted);
    case 'ticket_valid_unconfirmed':
      return t(($) => $.desktop.perimeter.ticket_valid_unconfirmed);
    case 'ticket_valid_unconfirmed_outside_perimeter':
      return t(($) => $.desktop.perimeter.ticket_valid_unconfirmed_outside_perimeter);
    case 'principal_mismatch':
      return t(($) => $.desktop.perimeter.principal_mismatch);
    case 'ticket_expiring_soon':
      return t(($) => $.desktop.perimeter.ticket_expiring_soon);
    case 'ticket_expired':
      return t(($) => $.desktop.perimeter.ticket_expired);
    case 'ticket_none':
      return t(($) => $.desktop.perimeter.ticket_none);
    case 'ticket_unknown':
      return t(($) => $.desktop.perimeter.ticket_unknown);
    case 'live_server_ok':
      return t(($) => $.desktop.perimeter.live_server_ok);
    case 'live_server_degraded':
      return t(($) => $.desktop.perimeter.live_server_degraded);
    case 'live_server_unreachable':
      return t(($) => $.desktop.perimeter.live_server_unreachable);
    case 'live_server_unconfigured':
      return t(($) => $.desktop.perimeter.live_server_unconfigured);
    case 'live_llm_ok':
      return t(($) => $.desktop.perimeter.live_llm_ok);
    case 'live_llm_degraded':
      return t(($) => $.desktop.perimeter.live_llm_degraded);
    case 'live_llm_unreachable':
      return t(($) => $.desktop.perimeter.live_llm_unreachable);
    case 'live_llm_unconfigured':
    case 'llm_key_not_configured':
      return t(($) => $.desktop.perimeter.live_llm_unconfigured);
    case 'live_daemon_ok':
      return t(($) => $.desktop.perimeter.live_daemon_ok);
    case 'live_daemon_stopped':
      return t(($) => $.desktop.perimeter.live_daemon_stopped);
    case 'live_daemon_unknown':
      return t(($) => $.desktop.perimeter.live_daemon_unknown);
    case 'live_provisioning_ok':
      return t(($) => $.desktop.perimeter.live_provisioning_ok);
    case 'live_provisioning_pending':
      return t(($) => $.desktop.perimeter.live_provisioning_pending);
    case 'live_provisioning_restart_pending':
      return t(($) => $.desktop.perimeter.live_provisioning_restart_pending);
    case 'live_provisioning_fail':
      return t(($) => $.desktop.perimeter.live_provisioning_fail);
    case 'live_provisioning_unknown':
      return t(($) => $.desktop.perimeter.live_provisioning_unknown);
    default:
      return t(($) => $.desktop.perimeter.live_overall_unknown);
  }
}

function ticketSourceLabel(t: PerimeterT, source: TicketSource): string {
  switch (source) {
    case 'kinit_app':
      return t(($) => $.desktop.perimeter.ticket_source_kinit_app);
    case 'renewed':
      return t(($) => $.desktop.perimeter.ticket_source_renewed);
    case 'cache':
      return t(($) => $.desktop.perimeter.ticket_source_cache);
  }
}

function liveOverallLabel(t: PerimeterT, overall: PerimeterCheckState): string {
  switch (overall) {
    case 'ok':
      return t(($) => $.desktop.perimeter.live_overall_ok);
    case 'degraded':
      return t(($) => $.desktop.perimeter.live_overall_degraded);
    case 'fail':
      return t(($) => $.desktop.perimeter.live_overall_fail);
    case 'unknown':
      return t(($) => $.desktop.perimeter.live_overall_unknown);
  }
}

function LiveCheckRow({
  t,
  check,
  state,
  uiLocale,
  restartBusy,
  onRestartNow,
}: {
  t: PerimeterT;
  check: PerimeterCheck;
  state: PerimeterStateView;
  uiLocale: string;
  restartBusy: boolean;
  onRestartNow?: () => void;
}) {
  return (
    <div
      className="flex flex-col gap-1.5"
      data-check-id={check.id}
      data-check-state={check.state}
      data-check-reason={check.reasonCode}
    >
      <div className="flex items-start gap-2 text-xs">
        <CheckStateIcon state={check.state} />
        <div className="flex min-w-0 flex-col">
          <span className="font-medium text-foreground">
            {liveItemLabel(t, check.id)}
            {check.counts ? `: ${check.counts.installed}/${check.counts.total}` : ''}
          </span>
          <span className="text-muted-foreground">{liveReasonLabel(t, check.reasonCode)}</span>
          {/* The address the row is about — the answer to "which proxy did the
           *  system choose for this". An address, never a credential: a PAC
           *  directive can carry userinfo, so the string is redacted here
           *  rather than trusted, and the sign-in screen opens this panel by
           *  itself (#229), which is what turned that into a live exposure. */}
          {check.detail && (
            <span className="break-all font-mono text-[11px] text-muted-foreground">
              {redactAddressCredentials(check.detail)}
            </span>
          )}
        </div>
      </div>
      {/* T-31 / ADR-0022: the panel must say WHOSE ticket this is, until when,
       *  and how it got here — a green row alone is exactly what let a ticket
       *  bought for the wrong account look fine (#283). */}
      {check.id === 'kerberos' && (
        <div className="ml-5 flex flex-col gap-0.5">
          {state.kerberosCachePrincipal && (
            <p className="break-all text-[11px] text-muted-foreground">
              {t(($) => $.desktop.perimeter.kerberos_cache_principal, {
                principal: state.kerberosCachePrincipal,
              })}
            </p>
          )}
          {state.kerberosExpiresAt !== null && state.kerberosExpiresAt !== undefined && (
            <p className="text-[11px] text-muted-foreground">
              {t(($) => $.desktop.perimeter.kerberos_expires_at, {
                time: formatTicketExpiry(state.kerberosExpiresAt, uiLocale),
              })}
            </p>
          )}
          {state.ticketSource && (
            <p className="text-[11px] text-muted-foreground">
              {ticketSourceLabel(t, state.ticketSource)}
            </p>
          )}
        </div>
      )}
      {check.id === 'provisioning' && check.restartPending === true && onRestartNow && (
        <div className="ml-5 flex flex-col gap-1">
          <p className="text-xs text-muted-foreground">
            {t(($) => $.desktop.perimeter.provisioning_restart_pending_note)}
          </p>
          <Button
            variant="outline"
            size="sm"
            className="w-fit"
            disabled={restartBusy}
            onClick={onRestartNow}
          >
            {restartBusy && <Loader2 className="size-3.5 animate-spin" />}
            {t(($) => $.desktop.perimeter.provisioning_restart_action)}
          </Button>
        </div>
      )}
      {check.id === 'provisioning' &&
        check.preservedLegacyPaths &&
        check.preservedLegacyPaths.length > 0 && (
          <div className="ml-5 flex flex-col gap-0.5">
            <p className="text-xs font-medium text-foreground">
              {t(($) => $.desktop.perimeter.provisioning_legacy_row_label)}
            </p>
            {check.preservedLegacyPaths.map((path) => (
              <p key={path} className="break-all font-mono text-[11px] text-muted-foreground">
                {path}
              </p>
            ))}
          </div>
        )}
      {check.id === 'provisioning' && check.removedPackages && check.removedPackages.length > 0 && (
        <div className="ml-5 flex flex-col gap-0.5">
          <p className="text-xs font-medium text-foreground">
            {t(($) => $.desktop.perimeter.provisioning_removed_row_label)}
          </p>
          {check.removedPackages.map((label) => (
            <p key={label} className="break-all font-mono text-[11px] text-muted-foreground">
              {label}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

export function NetworkStatusPanel({
  state,
  busy,
  onRecheck,
  restartBusy = false,
  onRestartNow,
}: {
  state: PerimeterStateView;
  busy: boolean;
  onRecheck: () => void;
  restartBusy?: boolean;
  onRestartNow?: () => void;
}) {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const live = state.live ?? null;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <CheckStateIcon state={live?.overall ?? 'unknown'} />
          <span
            className={cn('text-xs font-medium', checkStateToneClass(live?.overall ?? 'unknown'))}
          >
            {liveOverallLabel(t, live?.overall ?? 'unknown')}
          </span>
        </div>
        <button
          type="button"
          onClick={onRecheck}
          disabled={busy}
          aria-label={t(($) => $.desktop.perimeter.recheck)}
          title={t(($) => $.desktop.perimeter.recheck)}
          className="inline-flex size-5 items-center justify-center rounded text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
        >
          <RefreshCw className={cn('size-3.5', busy && 'animate-spin')} />
        </button>
      </div>
      <div className="flex flex-col gap-2">
        {(live?.checks ?? []).map((check) => (
          <LiveCheckRow
            key={check.id}
            t={t}
            check={check}
            state={state}
            uiLocale={uiLocale}
            restartBusy={restartBusy}
            onRestartNow={onRestartNow}
          />
        ))}
      </div>
    </div>
  );
}

export function useNetworkStatus(options: { subscribe?: boolean } = {}) {
  const subscribe = options.subscribe !== false;
  const [state, setState] = useState<PerimeterStateView | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setState((await window.daemonAPI?.getPerimeter?.()) ?? null);
    } catch {
      setState(null);
    }
  }, []);

  const recheck = useCallback(async () => {
    setBusy(true);
    try {
      const next = await window.daemonAPI?.recheckPerimeter?.();
      if (next) setState(next);
      else await refresh();
    } catch {
      await refresh();
    } finally {
      setBusy(false);
    }
  }, [refresh]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!subscribe) return;
    return window.daemonAPI?.onPerimeterState?.((next) => setState(next));
  }, [subscribe]);

  return { state, busy, refresh, recheck, setState };
}

export function formatTicketExpiry(expiresAt: number, locale: string): string {
  return new Date(expiresAt).toLocaleTimeString(locale, {
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatTicketDeadline(at: number, locale: string): string {
  return new Date(at).toLocaleString(locale, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function useKinitActions(
  setState: (next: PerimeterStateView) => void,
  principal: string | null = null,
) {
  const { t } = useT('settings');
  const [kinitOpen, setKinitOpen] = useState(false);
  const [renewBusy, setRenewBusy] = useState(false);

  const renew = useCallback(async () => {
    setRenewBusy(true);
    try {
      const result = await window.daemonAPI?.perimeterKinitRenew?.(principal);
      if (result?.ok) {
        setState(result.state);
        toast.success(t(($) => $.desktop.perimeter.kinit_success));
        return;
      }
      toast.warning(t(($) => $.desktop.perimeter.kerberos_renew_failed));
      setKinitOpen(true);
    } catch {
      toast.warning(t(($) => $.desktop.perimeter.kerberos_renew_failed));
      setKinitOpen(true);
    } finally {
      setRenewBusy(false);
    }
  }, [principal, setState, t]);

  return { kinitOpen, setKinitOpen, renew, renewBusy };
}

export function KerberosTicketBanner() {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const { state, setState } = useNetworkStatus();
  const { principal } = useResolvedKerberosPrincipal(state);
  const { kinitOpen, setKinitOpen, renew, renewBusy } = useKinitActions(setState, principal);

  if (!state || state.kerberosSupported !== true) return null;
  if (state.caBundlePresent !== true && state.corpCaPresent !== true) {
    return null;
  }
  const ticket = state.kerberosTicket;
  if (ticket !== 'none' && ticket !== 'expired' && ticket !== 'expiring_soon') {
    return null;
  }

  const message =
    ticket === 'none'
      ? t(($) => $.desktop.perimeter.kerberos_banner_none)
      : ticket === 'expired'
        ? t(($) => $.desktop.perimeter.kerberos_banner_expired)
        : t(($) => $.desktop.perimeter.kerberos_banner_expiring, {
            time:
              state.kerberosExpiresAt !== null
                ? formatTicketExpiry(state.kerberosExpiresAt, uiLocale)
                : '—',
          });

  return (
    <>
      <div
        data-testid="kerberos-ticket-banner"
        data-ticket-state={ticket}
        className={cn(
          'flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs',
          ticket === 'expiring_soon'
            ? 'bg-warning/10 text-warning'
            : 'bg-destructive/10 text-destructive',
        )}
      >
        <ShieldAlert className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate">{message}</span>
        {ticket !== 'none' && (
          <Button
            variant="outline"
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={renewBusy}
            onClick={() => void renew()}
          >
            {renewBusy && <Loader2 className="size-3 animate-spin" />}
            {t(($) => $.desktop.perimeter.kerberos_renew)}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          className="h-6 px-2 text-xs"
          onClick={() => setKinitOpen(true)}
        >
          <KeyRound className="size-3" />
          {t(($) => $.desktop.perimeter.get_ticket)}
        </Button>
      </div>
      <KinitDialog
        open={kinitOpen}
        onOpenChange={setKinitOpen}
        principal={principal}
        onSuccess={(next) => setState(next)}
      />
    </>
  );
}

export function NetworkStatusIndicator() {
  const { t } = useT('settings');
  const { state, busy, refresh, recheck, setState } = useNetworkStatus();
  const { principal } = useResolvedKerberosPrincipal(state);
  const { preferences, save } = useKerberosPreferences();
  const [restartBusy, setRestartBusy] = useState(false);
  const [kinitOpen, setKinitOpen] = useState(false);
  const promptedRef = useRef(false);
  const warnedSoonRef = useRef(false);
  const renewedRef = useRef(false);

  useEffect(() => {
    if (!state || state.kerberosSupported !== true) return;
    if (state.kerberosTicket === 'valid') {
      promptedRef.current = false;
      warnedSoonRef.current = false;
      renewedRef.current = false;
      return;
    }
    if (state.kerberosTicket === 'unknown') return;

    const promptOnce = () => {
      warnedSoonRef.current = false;
      if (promptedRef.current) return;
      promptedRef.current = true;
      setKinitOpen(true);
    };

    const action = decideKerberosAction({
      nowMs: Date.now(),
      expiresAt: state.kerberosExpiresAt,
      renewUntil: state.kerberosRenewUntil ?? null,
      lastKinitAt: state.lastKinit?.ok === true ? state.lastKinit.at : null,
      thresholdMs: preferences.thresholdMs,
      snoozedUntil: preferences.snoozedUntil,
    });
    if (action === 'prompt') {
      promptOnce();
      return;
    }
    if (action === 'renew' && !renewedRef.current) {
      renewedRef.current = true;
      const snoozed = preferences.snoozedUntil !== null && Date.now() < preferences.snoozedUntil;
      void (async () => {
        try {
          const result = await window.daemonAPI?.perimeterKinitRenew?.(principal);
          if (result?.ok === true) {
            setState(result.state);
            return;
          }
        } catch {
          // Treated exactly like a rejected renewal.
        }
        if (!snoozed) promptOnce();
      })();
    }
    if (state.kerberosTicket === 'expiring_soon' && !warnedSoonRef.current) {
      warnedSoonRef.current = true;
      toast.warning(t(($) => $.desktop.perimeter.ticket_expiring_soon_warn));
    }
  }, [state, principal, preferences.thresholdMs, preferences.snoozedUntil, setState, t]);

  const restartProvisioningNow = useCallback(async () => {
    setRestartBusy(true);
    try {
      const outcome = await window.daemonAPI?.restartProvisioningNow?.();
      if (outcome === 'deferred') {
        toast.warning(t(($) => $.desktop.perimeter.provisioning_restart_busy));
      } else if (outcome === 'restarted') {
        toast.success(t(($) => $.desktop.perimeter.provisioning_restart_success));
      }
      await refresh();
    } catch {
      toast.error(t(($) => $.desktop.perimeter.provisioning_restart_failed));
    } finally {
      setRestartBusy(false);
    }
  }, [t, refresh]);

  if (state === null) return null;

  const overall = state.live?.overall ?? 'unknown';
  const TriggerIcon = overall === 'ok' ? ShieldCheck : overall === 'fail' ? ShieldAlert : Shield;
  const showTicketAction = state.kerberosSupported === true && state.kerberosTicket !== 'valid';

  return (
    <>
      <Popover>
        <PopoverTrigger
          aria-label={t(($) => $.desktop.perimeter.sidebar_aria)}
          title={`${t(($) => $.desktop.perimeter.title)}: ${liveOverallLabel(t, overall)}`}
          className={cn(
            'inline-flex size-7 items-center justify-center rounded-full transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent',
            checkStateToneClass(overall),
          )}
        >
          <TriggerIcon className="size-4" />
        </PopoverTrigger>
        <PopoverContent side="top" align="end" className="w-80 gap-3">
          <div className="min-w-0">
            <p className="text-sm font-medium">{t(($) => $.desktop.perimeter.title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.desktop.perimeter.description)}
            </p>
          </div>

          <div className="border-t border-border pt-2.5">
            <NetworkStatusPanel
              state={state}
              busy={busy}
              onRecheck={() => void recheck()}
              restartBusy={restartBusy}
              onRestartNow={() => void restartProvisioningNow()}
            />
          </div>

          <DoctorSection />

          {showTicketAction && (
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={() => setKinitOpen(true)}
            >
              <KeyRound className="size-3.5" />
              {t(($) => $.desktop.perimeter.get_ticket)}
            </Button>
          )}
        </PopoverContent>
      </Popover>

      <KinitDialog
        open={kinitOpen}
        onOpenChange={setKinitOpen}
        principal={principal}
        onSnooze={() => void save({ snoozedUntil: Date.now() + KERBEROS_SNOOZE_MS })}
        onSuccess={(next) => setState(next)}
      />
    </>
  );
}

export function KinitDialog({
  open,
  onOpenChange,
  principal,
  reason,
  onSnooze,
  onSuccess,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  principal: string | null;
  reason?: string | null;
  onSnooze?: () => void;
  onSuccess: (state: PerimeterStateView) => void;
}) {
  const { t } = useT('settings');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [askPrincipal, setAskPrincipal] = useState(false);
  const [principalInput, setPrincipalInput] = useState('');

  useEffect(() => {
    if (!open) {
      setPassword('');
      setAskPrincipal(false);
      setPrincipalInput('');
    }
  }, [open]);

  const submit = useCallback(async () => {
    const typedRaw = askPrincipal || principal === null ? principalInput : null;
    const typed =
      typedRaw !== null && typedRaw.trim().length > 0
        ? validateKerberosPrincipalInput(typedRaw)
        : null;
    if (typed && !typed.ok) {
      toast.error(
        typed.issue === 'whitespace'
          ? t(($) => $.desktop.perimeter.kerberos_principal_whitespace)
          : t(($) => $.desktop.perimeter.kerberos_principal_shape),
      );
      return;
    }
    const effective = typed?.value ?? (typedRaw === null ? principal : null);
    if (password.length === 0) return;
    setSubmitting(true);
    try {
      const result = await window.daemonAPI.perimeterKinit(effective, password);
      if (result.ok) {
        setPassword('');
        if (typed && !(await saveKerberosPrincipalOverride(typed.value))) {
          toast.warning(t(($) => $.desktop.perimeter.kerberos_principal_save_failed));
        }
        onSuccess(result.state);
        onOpenChange(false);
        toast.success(t(($) => $.desktop.perimeter.kinit_success));
      } else {
        if (result.reason === 'principal_unknown' && !askPrincipal) {
          setAskPrincipal(true);
          setPrincipalInput(principal ?? '');
        } else {
          setPassword('');
        }
        toast.error(kinitFailureMessage(t, result.reason, result.message));
      }
    } finally {
      setSubmitting(false);
    }
  }, [principal, password, askPrincipal, principalInput, onSuccess, onOpenChange, t]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.desktop.perimeter.kinit_title)}</DialogTitle>
          <DialogDescription>
            {reason ?? t(($) => $.desktop.perimeter.kinit_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          {principal === null ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="kinit-principal">
                {t(($) => $.desktop.perimeter.kerberos_principal_label)}
              </Label>
              <Input
                id="kinit-principal"
                value={principalInput}
                autoFocus
                spellCheck={false}
                autoCapitalize="none"
                placeholder={t(($) => $.desktop.perimeter.kinit_principal_placeholder)}
                onChange={(event) => setPrincipalInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') void submit();
                }}
              />
            </div>
          ) : askPrincipal ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="kinit-principal">
                {t(($) => $.desktop.perimeter.kerberos_principal_label)}
              </Label>
              <Input
                id="kinit-principal"
                value={principalInput}
                autoFocus
                spellCheck={false}
                autoCapitalize="none"
                onChange={(event) => setPrincipalInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') void submit();
                }}
              />
              <p className="text-xs text-muted-foreground">
                {t(($) => $.desktop.perimeter.kinit_principal_ask)}
              </p>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label>{t(($) => $.desktop.perimeter.kinit_principal_label)}</Label>
              <p className="break-all font-mono text-sm text-muted-foreground">{principal}</p>
            </div>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kinit-password">
              {t(($) => $.desktop.perimeter.kinit_password_label)}
            </Label>
            <Input
              id="kinit-password"
              type="password"
              value={password}
              autoFocus={!askPrincipal && principal !== null}
              onChange={(event) => setPassword(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void submit();
              }}
            />
          </div>
        </div>

        <DialogFooter>
          {onSnooze && (
            <Button
              variant="ghost"
              onClick={() => {
                onSnooze();
                onOpenChange(false);
              }}
            >
              {t(($) => $.desktop.perimeter.kerberos_snooze)}
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.desktop.perimeter.kinit_cancel)}
          </Button>
          <Button
            id="kinit-submit"
            onClick={() => void submit()}
            disabled={
              password.length === 0 ||
              (askPrincipal && principalInput.trim().length === 0) ||
              submitting
            }
          >
            {submitting && <Loader2 className="size-3.5 animate-spin" />}
            {submitting
              ? t(($) => $.desktop.perimeter.kinit_submitting)
              : t(($) => $.desktop.perimeter.kinit_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
