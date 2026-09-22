import { useCallback, useEffect, useRef, useState } from 'react';

import { useAuthStore } from '@goosar/core/auth';
import { setTaskPreflight } from '@goosar/core/platform';
import { useT } from '@goosar/views/i18n';

import {
  decideKerberosAction,
  isKerberosBackedInstalled,
  KERBEROS_DEFAULT_THRESHOLD_MS,
  resolveKerberosPrincipal,
} from '../../../shared/kerberos-identity';
import type { KerberosPreferences } from '../../../shared/kerberos-preferences-types';
import { KinitDialog } from './network-status';

const FALLBACK_PREFERENCES: KerberosPreferences = {
  principal: null,
  thresholdMs: KERBEROS_DEFAULT_THRESHOLD_MS,
  snoozedUntil: null,
};

export function KerberosPreflightGate() {
  const { t } = useT('settings');
  const email = useAuthStore((s) => s.user?.email ?? null);

  const [open, setOpen] = useState(false);
  const [principal, setPrincipal] = useState<string | null>(null);
  const [reason, setReason] = useState<string | null>(null);
  const pendingRef = useRef<((allowed: boolean) => void) | null>(null);

  const settle = useCallback((allowed: boolean) => {
    const resolve = pendingRef.current;
    pendingRef.current = null;
    setOpen(false);
    resolve?.(allowed);
  }, []);

  useEffect(() => {
    setTaskPreflight(async () => {
      const state = await window.daemonAPI?.getPerimeter?.();
      if (!state || state.kerberosSupported !== true) return true;

      const provisioning = await window.daemonAPI?.getProvisioningStatus?.();
      const installed = (provisioning?.packages ?? []).map((p) => p.name);
      if (!isKerberosBackedInstalled(installed)) return true;

      const preferences =
        (await window.daemonAPI?.kerberosGetPreferences?.()) ?? FALLBACK_PREFERENCES;
      const resolved = resolveKerberosPrincipal({
        override: preferences.principal,
        email,
        realm: state.realm,
        principalDomain: state.principalDomain ?? null,
      });

      const action = decideKerberosAction({
        nowMs: Date.now(),
        expiresAt: state.kerberosExpiresAt,
        renewUntil: state.kerberosRenewUntil ?? null,
        lastKinitAt: state.lastKinit?.ok === true ? state.lastKinit.at : null,
        thresholdMs: preferences.thresholdMs,
        snoozedUntil: preferences.snoozedUntil,
        preflight: true,
      });
      if (action === 'none') return true;

      if (action === 'renew') {
        const renewed = await window.daemonAPI?.perimeterKinitRenew?.(resolved);
        if (renewed?.ok === true) return true;
      }

      setPrincipal(resolved);
      setReason(t(($) => $.desktop.perimeter.kerberos_reason_preflight));
      setOpen(true);
      return await new Promise<boolean>((resolve) => {
        pendingRef.current?.(true);
        pendingRef.current = resolve;
      });
    });
    return () => setTaskPreflight(null);
  }, [email, t]);

  return (
    <KinitDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) settle(false);
      }}
      principal={principal}
      reason={reason}
      onSuccess={() => settle(true)}
    />
  );
}
