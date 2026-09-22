import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';

import { useAuthStore } from '@goosar/core/auth';

import { resolveKerberosPrincipal } from '../../../shared/kerberos-identity';
import type { KerberosPreferences } from '../../../shared/kerberos-preferences-types';
import { KERBEROS_DEFAULT_THRESHOLD_MS } from '../../../shared/kerberos-identity';
import type { PerimeterStateView } from '../../../shared/perimeter-config';

const DEFAULTS: KerberosPreferences = {
  principal: null,
  thresholdMs: KERBEROS_DEFAULT_THRESHOLD_MS,
  snoozedUntil: null,
};

let snapshot: KerberosPreferences = DEFAULTS;
const listeners = new Set<() => void>();

function publish(next: KerberosPreferences): void {
  snapshot = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useKerberosPreferences() {
  const preferences = useSyncExternalStore(subscribe, () => snapshot);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const stored = await window.daemonAPI?.kerberosGetPreferences?.();
        if (!cancelled && stored) publish(stored);
      } catch {
        // Keep the defaults: an unreadable preference file must not stop a
        // user from signing in.
      } finally {
        if (!cancelled) setLoaded(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const save = useCallback(
    async (patch: Partial<KerberosPreferences>): Promise<KerberosPreferences> => {
      const next = { ...snapshot, ...patch };
      try {
        const stored = await window.daemonAPI?.kerberosSetPreferences?.(next);
        const adopted = stored ?? next;
        publish(adopted);
        return adopted;
      } catch {
        publish(next);
        return next;
      }
    },
    [],
  );

  return { preferences, loaded, save };
}

export async function saveKerberosPrincipalOverride(principal: string): Promise<boolean> {
  try {
    const current = (await window.daemonAPI?.kerberosGetPreferences?.()) ?? snapshot;
    const next = { ...current, principal };
    const stored = await window.daemonAPI?.kerberosSetPreferences?.(next);
    publish(stored ?? next);
    return stored?.principal === principal;
  } catch {
    return false;
  }
}

export function useResolvedKerberosPrincipal(state: PerimeterStateView | null) {
  const email = useAuthStore((s) => s.user?.email ?? null);
  const prefs = useKerberosPreferences();
  const principal = resolveKerberosPrincipal({
    override: prefs.preferences.principal,
    email,
    realm: state?.realm ?? null,
    principalDomain: state?.principalDomain ?? null,
  });
  useEffect(() => {
    void window.daemonAPI?.kerberosSetResolvedPrincipal?.(principal);
  }, [principal]);
  return {
    ...prefs,
    derived: resolveKerberosPrincipal({
      email,
      realm: state?.realm ?? null,
      principalDomain: state?.principalDomain ?? null,
    }),
    principal,
  };
}
