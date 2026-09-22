// Схема `runtime_config`, специфичная для Runtime-N.

export type RuntimeNRoutingMode = 'local' | 'gateway';

export interface RuntimeNGatewayPin {
  host?: string;
  port?: number;
  token?: string;
  tls?: boolean;
}

export interface RuntimeNConfig {
  mode?: RuntimeNRoutingMode;
  gateway?: RuntimeNGatewayPin;
}

export const RUNTIME_N_GATEWAY_TOKEN_MASK = '***';

export function parseRuntimeNConfig(raw: unknown): RuntimeNConfig {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
  const root = raw as Record<string, unknown>;
  const out: RuntimeNConfig = {};
  if (root.mode === 'local' || root.mode === 'gateway') {
    out.mode = root.mode;
  }
  if (root.gateway && typeof root.gateway === 'object' && !Array.isArray(root.gateway)) {
    const gw = root.gateway as Record<string, unknown>;
    const pin: RuntimeNGatewayPin = {};
    if (typeof gw.host === 'string' && gw.host !== '') pin.host = gw.host;
    if (typeof gw.port === 'number' && Number.isFinite(gw.port) && gw.port > 0) pin.port = gw.port;
    if (typeof gw.token === 'string' && gw.token !== '') pin.token = gw.token;
    if (typeof gw.tls === 'boolean') pin.tls = gw.tls;
    if (Object.keys(pin).length > 0) out.gateway = pin;
  }
  return out;
}

export function serializeRuntimeNConfig(cfg: RuntimeNConfig): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (cfg.mode) out.mode = cfg.mode;
  if (cfg.gateway) {
    const gw: Record<string, unknown> = {};
    if (cfg.gateway.host) gw.host = cfg.gateway.host;
    if (cfg.gateway.port) gw.port = cfg.gateway.port;
    if (cfg.gateway.tls) gw.tls = true;
    if (cfg.gateway.token) {
      gw.token = cfg.gateway.token;
    }
    if (Object.keys(gw).length > 0) out.gateway = gw;
  }
  return out;
}

export function runtimeNConfigEquals(a: RuntimeNConfig, b: RuntimeNConfig): boolean {
  if ((a.mode ?? 'local') !== (b.mode ?? 'local')) return false;
  const aGw = a.gateway ?? {};
  const bGw = b.gateway ?? {};
  if ((aGw.host ?? '') !== (bGw.host ?? '')) return false;
  if ((aGw.port ?? 0) !== (bGw.port ?? 0)) return false;
  if ((aGw.token ?? '') !== (bGw.token ?? '')) return false;
  if (Boolean(aGw.tls) !== Boolean(bGw.tls)) return false;
  return true;
}
