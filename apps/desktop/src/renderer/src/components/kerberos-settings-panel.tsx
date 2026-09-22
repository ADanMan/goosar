import { useCallback, useEffect, useRef, useState } from 'react';
import { KeyRound, Loader2, RefreshCw } from 'lucide-react';
import { toast } from 'sonner';

import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { useT, useUiLocale } from '@goosar/views/i18n';

import { validateKerberosPrincipalInput } from '../../../shared/kerberos-identity';
import {
  formatTicketDeadline,
  formatTicketExpiry,
  KinitDialog,
  useKinitActions,
  useNetworkStatus,
} from './network-status';
import { useResolvedKerberosPrincipal } from './use-kerberos-principal';

const THRESHOLD_CHOICES_MIN = [15, 30, 60, 120] as const;

export function KerberosSignInPanel() {
  const { t } = useT('settings');
  const uiLocale = useUiLocale();
  const { state, busy, recheck, setState } = useNetworkStatus();
  const { preferences, loaded, save, principal, derived } = useResolvedKerberosPrincipal(state);

  const { kinitOpen, setKinitOpen, renew, renewBusy } = useKinitActions(setState, principal);

  const [draft, setDraft] = useState('');
  const [saving, setSaving] = useState(false);
  const seededRef = useRef('');
  useEffect(() => {
    if (!loaded) return;
    const stored = preferences.principal ?? '';
    if (draft !== seededRef.current && draft !== stored) return;
    seededRef.current = stored;
    if (draft !== stored) setDraft(stored);
  }, [loaded, preferences.principal, draft]);

  const check = validateKerberosPrincipalInput(draft);
  const isDirty = draft.trim() !== (preferences.principal ?? '');

  const canSave = isDirty && (check.ok || draft.trim().length === 0);

  const savePrincipal = useCallback(async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const stored = await save({ principal: check.ok ? check.value : null });
      toast.success(
        stored.principal === null
          ? t(($) => $.desktop.perimeter.kerberos_principal_reset_done)
          : t(($) => $.desktop.perimeter.kerberos_principal_saved),
      );
    } finally {
      setSaving(false);
    }
  }, [canSave, check.ok, check.value, save, t]);

  if (state === null || state.kerberosSupported !== true) return null;
  const ticket = state.kerberosTicket;

  const statusLabel =
    ticket === 'valid'
      ? t(($) => $.desktop.perimeter.ticket_valid)
      : ticket === 'expiring_soon'
        ? t(($) => $.desktop.perimeter.ticket_expiring_soon)
        : ticket === 'expired'
          ? t(($) => $.desktop.perimeter.ticket_expired)
          : ticket === 'none'
            ? t(($) => $.desktop.perimeter.ticket_none)
            : t(($) => $.desktop.perimeter.ticket_unknown);

  const validationMessage =
    draft.trim().length === 0 || check.ok
      ? null
      : check.issue === 'whitespace'
        ? t(($) => $.desktop.perimeter.kerberos_principal_whitespace)
        : t(($) => $.desktop.perimeter.kerberos_principal_shape);

  const lastKinit = state.lastKinit ?? null;

  return (
    <div className="flex flex-col gap-4" data-ticket-state={ticket}>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="kerberos-principal">
          {t(($) => $.desktop.perimeter.kerberos_principal_label)}
        </Label>
        <Input
          id="kerberos-principal"
          value={draft}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          placeholder={derived ?? ''}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && canSave) void savePrincipal();
          }}
        />
        <p className="text-xs text-muted-foreground">
          {t(($) => $.desktop.perimeter.kerberos_principal_hint)}
        </p>
        {validationMessage !== null && (
          <p className="text-xs text-destructive" role="alert">
            {validationMessage}
          </p>
        )}
        {check.ok && check.realmNotUppercase && (
          <p className="text-xs text-warning" data-testid="kerberos-realm-case">
            {t(($) => $.desktop.perimeter.kerberos_principal_realm_case)}
          </p>
        )}
        <div className="flex gap-2 pt-1">
          <Button size="sm" disabled={!canSave || saving} onClick={() => void savePrincipal()}>
            {saving && <Loader2 className="size-3.5 animate-spin" />}
            {t(($) => $.desktop.perimeter.kerberos_principal_save)}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={saving || (draft.length === 0 && preferences.principal === null)}
            onClick={() => {
              setDraft('');
              void save({ principal: null });
            }}
          >
            {t(($) => $.desktop.perimeter.kerberos_principal_reset)}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="kerberos-threshold">
          {t(($) => $.desktop.perimeter.kerberos_threshold_label)}
        </Label>
        <select
          id="kerberos-threshold"
          className="h-8 w-fit rounded-md border border-input bg-background px-2 text-sm"
          value={String(Math.round(preferences.thresholdMs / 60_000))}
          onChange={(event) => void save({ thresholdMs: Number(event.target.value) * 60_000 })}
        >
          {THRESHOLD_CHOICES_MIN.map((minutes) => (
            <option key={minutes} value={minutes}>
              {t(($) => $.desktop.perimeter.kerberos_threshold_minutes, {
                count: minutes,
              })}
            </option>
          ))}
        </select>
      </div>

      <div
        className="flex flex-col gap-0.5 border-t border-border pt-3"
        data-testid="kerberos-observability"
      >
        <p className="text-sm">{statusLabel}</p>
        {principal !== null && (
          <p className="break-all text-xs text-muted-foreground">
            {t(($) => $.desktop.perimeter.kerberos_principal_in_use, {
              principal,
            })}
          </p>
        )}
        {typeof state.kerberosCachePrincipal === 'string' &&
          state.kerberosCachePrincipal.length > 0 && (
            <p className="break-all text-xs text-muted-foreground">
              {t(($) => $.desktop.perimeter.kerberos_cache_principal, {
                principal: state.kerberosCachePrincipal,
              })}
            </p>
          )}
        {state.kerberosExpiresAt !== null && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.desktop.perimeter.kerberos_expires_at, {
              time: formatTicketExpiry(state.kerberosExpiresAt, uiLocale),
            })}
          </p>
        )}
        {typeof state.kerberosRenewUntil === 'number' && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.desktop.perimeter.kerberos_renew_until, {
              time: formatTicketDeadline(state.kerberosRenewUntil, uiLocale),
            })}
          </p>
        )}
        {lastKinit && (
          <p className="text-xs text-muted-foreground">
            {lastKinit.ok
              ? t(($) => $.desktop.perimeter.kerberos_last_kinit_ok, {
                  time: formatTicketExpiry(lastKinit.at, uiLocale),
                })
              : t(($) => $.desktop.perimeter.kerberos_last_kinit_failed, {
                  time: formatTicketExpiry(lastKinit.at, uiLocale),
                })}
          </p>
        )}
      </div>

      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => setKinitOpen(true)}>
          <KeyRound className="size-3.5" />
          {t(($) => $.desktop.perimeter.get_ticket)}
        </Button>
        {(ticket === 'valid' || ticket === 'expiring_soon' || ticket === 'expired') && (
          <Button variant="outline" size="sm" disabled={renewBusy} onClick={() => void renew()}>
            {renewBusy && <Loader2 className="size-3.5 animate-spin" />}
            {t(($) => $.desktop.perimeter.kerberos_renew)}
          </Button>
        )}
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void recheck()}>
          <RefreshCw className={busy ? 'size-3.5 animate-spin' : 'size-3.5'} />
          {t(($) => $.desktop.perimeter.kerberos_check_now)}
        </Button>
      </div>

      <KinitDialog
        open={kinitOpen}
        onOpenChange={setKinitOpen}
        principal={principal}
        onSuccess={(next) => setState(next)}
      />
    </div>
  );
}
