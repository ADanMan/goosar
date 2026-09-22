import { KeyRound } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '@goosar/views/i18n';
import type { KerberosTicketState, PerimeterStateView } from '../../../shared/perimeter-config';
import { KinitDialog, useKinitActions, useNetworkStatus } from './network-status';
import { useResolvedKerberosPrincipal } from './use-kerberos-principal';

const HOUR_MS = 3_600_000;

export interface KerberosTicketSummary {
  kind: 'none' | 'expiring' | 'valid';
  hours: number | null;
  canRenew: boolean;
}

export function kerberosTicketSummary({
  state,
  nowMs,
}: {
  state: Pick<
    PerimeterStateView,
    | 'kerberosSupported'
    | 'caBundlePresent'
    | 'corpCaPresent'
    | 'kerberosTicket'
    | 'kerberosExpiresAt'
  > | null;
  nowMs: number;
}): KerberosTicketSummary | null {
  if (!state) return null;
  if (state.kerberosSupported !== true) return null;
  if (state.caBundlePresent !== true && state.corpCaPresent !== true) {
    return null;
  }

  const ticket: KerberosTicketState = state.kerberosTicket;
  const expiresAt = state.kerberosExpiresAt;
  const hours =
    typeof expiresAt === 'number' && expiresAt > nowMs
      ? Math.floor((expiresAt - nowMs) / HOUR_MS)
      : null;
  const wholeHours = hours !== null && hours >= 1 ? hours : null;

  switch (ticket) {
    case 'none':
    case 'expired':
      return { kind: 'none', hours: null, canRenew: false };
    case 'expiring_soon':
      return { kind: 'expiring', hours: wholeHours, canRenew: true };
    case 'valid':
      return { kind: 'valid', hours: wholeHours, canRenew: true };
    default:
      return null;
  }
}

export function KerberosTicketStatus({ className }: { className?: string }) {
  const { t } = useT('settings');
  const { state, setState } = useNetworkStatus();
  const { principal } = useResolvedKerberosPrincipal(state);
  const { kinitOpen, setKinitOpen, renew, renewBusy } = useKinitActions(setState, principal);

  const summary = kerberosTicketSummary({ state, nowMs: Date.now() });
  if (!summary) return null;

  const label =
    summary.kind === 'none'
      ? t(($) => $.desktop.perimeter.header_ticket_none)
      : summary.hours !== null
        ? t(($) => $.desktop.perimeter.header_ticket_hours, {
            count: summary.hours,
          })
        : t(($) => $.desktop.perimeter.header_ticket_present);

  return (
    <div
      data-testid="kerberos-ticket-status"
      data-ticket-kind={summary.kind}
      className={cn('flex items-center gap-1.5 text-xs', className)}
    >
      <KeyRound
        className={cn(
          'size-3.5',
          summary.kind === 'none'
            ? 'text-destructive'
            : summary.kind === 'expiring'
              ? 'text-warning'
              : 'text-muted-foreground',
        )}
        aria-hidden
      />
      <span className={cn(summary.kind === 'none' ? 'text-destructive' : 'text-muted-foreground')}>
        {label}
      </span>
      {/* A valid ticket needs no call to action; an expiring or missing one
          does. Renewal is tried first where it can work — `kinit -R` needs no
          password — and falls back to the dialog on its own. */}
      {summary.kind !== 'valid' ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-6 px-1.5 text-xs"
          disabled={renewBusy}
          onClick={() => {
            if (summary.canRenew) void renew();
            else setKinitOpen(true);
          }}
        >
          {t(($) => $.desktop.perimeter.get_ticket)}
        </Button>
      ) : null}
      <KinitDialog
        open={kinitOpen}
        onOpenChange={setKinitOpen}
        principal={principal}
        onSuccess={setState}
      />
    </div>
  );
}
