import { useCallback, useEffect, useRef, useState } from 'react';
import { LoginPage } from '@goosar/views/auth';
import { useAuthStore } from '@goosar/core/auth';
import { DragStrip } from '@goosar/views/platform';
import { useT } from '@goosar/views/i18n';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { ChevronDown } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { NetworkStatusPanel, useNetworkStatus } from '../components/network-status';
import {
  endpointPinsReplacedBy,
  type ReplacedEndpointPin,
  type RuntimeConfig,
} from '../../../shared/runtime-config';
import type { PerimeterStateView } from '../../../shared/perimeter-config';
import {
  classifyLoginTransportFailure,
  machineReportsNoNetwork,
  shouldRevealDiagnostics,
  goosarRouteCheck,
} from './login-network-failure';

export { isServerUnreachableError } from './login-network-failure';

const SERVER_FIELD_ID = 'desktop-server-url';

function requireRuntimeConfig(): RuntimeConfig {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      'Invariant violated: DesktopLoginPage rendered before App accepted runtime config',
    );
  }
  return runtimeConfig.config;
}

type ServerStatus =
  | { kind: 'idle' }
  | { kind: 'checking'; address: string }
  | { kind: 'unreachable'; address: string; detail: string }
  /** Reachable, but applying it would discard operator-pinned endpoints. */
  | { kind: 'replacing_pins'; address: string; replaced: ReplacedEndpointPin[] }
  | { kind: 'failed'; message: string }
  | { kind: 'applying'; address: string };

function DesktopServerField({ config }: { config: RuntimeConfig }) {
  const { t } = useT('auth');
  const apiUrl = config.apiUrl;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(apiUrl);
  const [status, setStatus] = useState<ServerStatus>({ kind: 'idle' });
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    setStatus({ kind: 'checking', address: apiUrl });
    void window.desktopAPI
      .probeRuntimeServer(apiUrl)
      .then((result) => {
        if (cancelled) return;
        setStatus(
          result.ok
            ? { kind: 'idle' }
            : {
                kind: 'unreachable',
                address: result.address,
                detail: result.message,
              },
        );
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setStatus({
          kind: 'unreachable',
          address: apiUrl,
          detail: err instanceof Error ? err.message : String(err),
        });
      });
    return () => {
      cancelled = true;
    };
  }, [apiUrl]);

  const apply = useCallback(async (address: string) => {
    const saved = await window.desktopAPI.setRuntimeConfig({ apiUrl: address });
    if (!mountedRef.current) return;
    if (!saved.ok) {
      setStatus({ kind: 'failed', message: saved.error.message });
      return;
    }
    setStatus({ kind: 'applying', address: saved.config.apiUrl });
    setEditing(false);
  }, []);

  const handleConnect = useCallback(async () => {
    if (status.kind === 'replacing_pins') {
      const confirmed = status.address;
      setStatus({ kind: 'checking', address: confirmed });
      await apply(confirmed);
      return;
    }

    const candidate = draft.trim();
    setStatus({ kind: 'checking', address: candidate });

    const probe = await window.desktopAPI.probeRuntimeServer(candidate);
    if (!mountedRef.current) return;
    if (!probe.ok) {
      setStatus({
        kind: 'unreachable',
        address: probe.address,
        detail: probe.message,
      });
      return;
    }

    const replaced = endpointPinsReplacedBy(config, probe.address);
    if (replaced.length > 0) {
      setStatus({ kind: 'replacing_pins', address: probe.address, replaced });
      return;
    }

    await apply(probe.address);
  }, [apply, config, draft, status]);

  const busy = status.kind === 'checking' || status.kind === 'applying';

  return (
    <div className="w-full space-y-2 text-left">
      {editing ? (
        <div className="space-y-2">
          <Label htmlFor={SERVER_FIELD_ID} className="text-xs text-muted-foreground">
            {t(($) => $.desktop.server.label)}
          </Label>
          <Input
            id={SERVER_FIELD_ID}
            type="url"
            inputMode="url"
            autoFocus
            value={draft}
            disabled={busy}
            placeholder={t(($) => $.desktop.server.placeholder)}
            onChange={(e) => {
              setDraft(e.target.value);
              setStatus((current) =>
                current.kind === 'replacing_pins' ? { kind: 'idle' } : current,
              );
            }}
          />
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              className="flex-1"
              disabled={busy || draft.trim().length === 0}
              onClick={() => void handleConnect()}
            >
              {status.kind === 'replacing_pins'
                ? t(($) => $.desktop.server.pins_confirm)
                : busy
                  ? t(($) => $.desktop.server.connecting)
                  : t(($) => $.desktop.server.connect)}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => {
                setDraft(apiUrl);
                setEditing(false);
                setStatus((current) =>
                  current.kind === 'replacing_pins' ? { kind: 'idle' } : current,
                );
              }}
            >
              {t(($) => $.desktop.server.cancel)}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">{t(($) => $.desktop.server.label)}</span>
          <span
            className="min-w-0 flex-1 truncate text-right text-xs text-foreground"
            title={apiUrl}
          >
            {apiUrl}
          </span>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-auto shrink-0 px-2 py-0.5 text-xs"
            onClick={() => {
              setDraft(apiUrl);
              setEditing(true);
            }}
          >
            {t(($) => $.desktop.server.change)}
          </Button>
        </div>
      )}

      {status.kind === 'checking' && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.desktop.server.checking, { address: status.address })}
        </p>
      )}
      {status.kind === 'unreachable' && (
        <div className="space-y-1">
          <p className="text-xs text-destructive">
            {t(($) => $.desktop.server.unreachable, { address: status.address })}
          </p>
          <p className="break-all text-xs text-muted-foreground">{status.detail}</p>
        </div>
      )}
      {status.kind === 'replacing_pins' && (
        <div className="space-y-1">
          <p className="text-xs text-warning">{t(($) => $.desktop.server.pins_intro)}</p>
          <ul className="space-y-0.5">
            {status.replaced.map((pin) => (
              <li key={pin.field} className="break-all font-mono text-xs text-muted-foreground">
                {pin.field}: {pin.previous} → {pin.next}
              </li>
            ))}
          </ul>
          <p className="text-xs text-muted-foreground">{t(($) => $.desktop.server.pins_outro)}</p>
        </div>
      )}
      {status.kind === 'failed' && (
        <p className="text-xs text-destructive">
          {t(($) => $.desktop.server.save_failed, { message: status.message })}
        </p>
      )}
      {status.kind === 'applying' && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.desktop.server.applying, { address: status.address })}
        </p>
      )}
    </div>
  );
}

function LoginNetworkDiagnostics({
  state,
  busy,
  recheck,
  open,
  onToggle,
}: {
  state: PerimeterStateView | null;
  busy: boolean;
  recheck: () => void;
  open: boolean;
  onToggle: () => void;
}) {
  const { t } = useT('settings');

  if (state === null) return null;

  return (
    <div className="mt-3 border-t border-border pt-3">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <span>{t(($) => $.desktop.perimeter.title)}</span>
        <ChevronDown className={cn('size-3.5 transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <div className="mt-2">
          <NetworkStatusPanel state={state} busy={busy} onRecheck={recheck} />
        </div>
      )}
    </div>
  );
}

export function DesktopLoginPage() {
  const { t } = useT('auth');
  const config = requireRuntimeConfig();
  const apiUrl = config.apiUrl;
  const { state, busy, recheck } = useNetworkStatus();
  const routeCheck = goosarRouteCheck(state);
  const [routeBreakSeen, setRouteBreakSeen] = useState(false);
  const [disclosure, setDisclosure] = useState<boolean | null>(null);
  const [offlineAtFailure, setOfflineAtFailure] = useState(false);
  const [corporateError, setCorporateError] = useState<string | null>(null);
  useEffect(() => {
    return window.desktopAPI.onAuthError((code) => setCorporateError(code));
  }, []);
  const pendingMfaToken = useAuthStore((state) => state.pendingMfaToken);
  const setPendingMfaToken = useAuthStore((state) => state.setPendingMfaToken);
  useEffect(() => {
    return window.desktopAPI.onAuthMfaToken((ticket) => {
      setCorporateError(null);
      setPendingMfaToken(ticket);
    });
  }, [setPendingMfaToken]);
  useEffect(() => () => setPendingMfaToken(null), [setPendingMfaToken]);

  const describeError = useCallback(
    (err: unknown) => {
      const transport = classifyLoginTransportFailure(err, routeCheck, offlineAtFailure);
      if (!transport) return undefined;
      switch (transport.kind) {
        case 'proxy':
          return t(($) => $.desktop.server.unreachable_proxy, {
            address: apiUrl,
            proxy: transport.proxy,
          });
        case 'proxy_unnamed':
          return t(($) => $.desktop.server.unreachable_proxy_unnamed, {
            address: apiUrl,
          });
        case 'proxy_candidates': {
          const paths = [...transport.proxies];
          if (transport.direct) {
            paths.push(t(($) => $.desktop.server.route_path_direct));
          }
          return t(($) => $.desktop.server.unreachable_proxy_candidates, {
            address: apiUrl,
            paths: paths.join(', '),
          });
        }
        case 'route_unknown':
          return t(($) => $.desktop.server.unreachable_route_unknown, {
            address: apiUrl,
          });
        case 'offline':
          return t(($) => $.desktop.server.unreachable_offline, {
            address: apiUrl,
          });
        case 'network_changed':
          return t(($) => $.desktop.server.unreachable_network_changed, {
            address: apiUrl,
          });
        case 'name_unresolved':
          return t(($) => $.desktop.server.unreachable_name, {
            address: apiUrl,
          });
        case 'direct':
          return t(($) => $.desktop.server.unreachable, { address: apiUrl });
      }
    },
    [apiUrl, offlineAtFailure, routeCheck, t],
  );

  const handleLoginError = useCallback(
    (err: unknown) => {
      const offline = machineReportsNoNetwork();
      setOfflineAtFailure(offline);
      if (shouldRevealDiagnostics(classifyLoginTransportFailure(err, routeCheck, offline))) {
        setRouteBreakSeen(true);
      }
    },
    [routeCheck],
  );

  const diagnosticsOpen = disclosure ?? routeBreakSeen;

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<GoosarIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        describeError={describeError}
        onError={handleLoginError}
        onCorporateLogin={(startUrl) => {
          setCorporateError(null);
          void window.desktopAPI.openExternal(startUrl);
        }}
        corporateClient="desktop"
        corporateErrorCode={corporateError}
        corporateMfaToken={pendingMfaToken}
        extra={
          <>
            <DesktopServerField config={config} />
            <LoginNetworkDiagnostics
              state={state}
              busy={busy}
              recheck={() => void recheck()}
              open={diagnosticsOpen}
              onToggle={() => setDisclosure(!diagnosticsOpen)}
            />
          </>
        }
      />
    </div>
  );
}
