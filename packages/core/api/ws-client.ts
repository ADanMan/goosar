import type { WSMessage, WSEventType } from '../types/events';
import { type Logger, noopLogger } from '../logger';

type EventHandler = (payload: unknown, actorId?: string, actorType?: string) => void;

const UNPARSEABLE_LOG_MAX_CHARS = 200;

const RECONNECT_BASE_DELAY_MS = 1_000;
const RECONNECT_MAX_DELAY_MS = 30_000;
const DEGRADED_AFTER_ATTEMPTS = 8;
const DEGRADED_RECONNECT_DELAY_MS = 2 * 60 * 1000;

export type WSConnectionState = 'connecting' | 'connected' | 'degraded' | 'unauthorized';

function summarizeUnparseable(data: unknown): string {
  const text = typeof data === 'string' ? data : String(data);
  if (text.length <= UNPARSEABLE_LOG_MAX_CHARS) return text;
  return `${text.slice(0, UNPARSEABLE_LOG_MAX_CHARS)}… (truncated, ${text.length} chars total)`;
}

export interface WSClientIdentity {
  platform?: string;
  version?: string;
  os?: string;
}

export class WSClient {
  private ws: WebSocket | null = null;
  private baseUrl: string;
  private token: string | null = null;
  private workspaceSlug: string | null = null;
  private cookieAuth = false;
  private identity: WSClientIdentity | undefined;
  private handlers = new Map<WSEventType, Set<EventHandler>>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private hasConnectedBefore = false;
  private badFrameLogged = false;
  private onReconnectCallbacks = new Set<() => void>();
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private logger: Logger;
  private state: WSConnectionState = 'connecting';
  private stateChangeCallbacks = new Set<(state: WSConnectionState) => void>();
  private onUnauthorizedCallbacks = new Set<(reason?: string) => void>();
  private awaitingAuthAck = false;
  private suppressReconnectOnNextClose = false;

  constructor(
    url: string,
    options?: {
      logger?: Logger;
      cookieAuth?: boolean;
      identity?: WSClientIdentity;
    },
  ) {
    this.baseUrl = url;
    this.logger = options?.logger ?? noopLogger;
    this.cookieAuth = options?.cookieAuth ?? false;
    this.identity = options?.identity;
  }

  setAuth(token: string | null, workspaceSlug: string) {
    this.token = token;
    this.workspaceSlug = workspaceSlug;
  }

  connect() {
    this.badFrameLogged = false;
    this.awaitingAuthAck = false;
    this.suppressReconnectOnNextClose = false;
    const url = new URL(this.baseUrl);
    if (this.workspaceSlug) url.searchParams.set('workspace_slug', this.workspaceSlug);
    if (this.identity?.platform) url.searchParams.set('client_platform', this.identity.platform);
    if (this.identity?.version) url.searchParams.set('client_version', this.identity.version);
    if (this.identity?.os) url.searchParams.set('client_os', this.identity.os);

    this.ws = new WebSocket(url.toString());

    this.ws.onopen = () => {
      if (!this.cookieAuth && this.token) {
        this.awaitingAuthAck = true;
        this.ws!.send(JSON.stringify({ type: 'auth', payload: { token: this.token } }));
        return;
      }

      this.onAuthenticated();
    };

    this.ws.onmessage = (event) => {
      let msg: WSMessage;
      try {
        msg = JSON.parse(event.data as string) as WSMessage;
      } catch {
        this.logger.warn('ws: received unparseable message', summarizeUnparseable(event.data));
        return;
      }

      const rawType = (msg as { type?: unknown } | null)?.type;
      const rawError = (msg as { error?: unknown } | null)?.error;

      if (rawType === 'auth_error' || (this.awaitingAuthAck && typeof rawError === 'string')) {
        this.awaitingAuthAck = false;
        this.handleAuthError(typeof rawError === 'string' ? rawError : undefined);
        return;
      }

      if (!msg || typeof rawType !== 'string') {
        if (!this.badFrameLogged) {
          this.badFrameLogged = true;
          this.logger.warn(
            'ws: dropping frame without a string type',
            summarizeUnparseable(event.data),
          );
        }
        return;
      }
      if (rawType === 'auth_ack') {
        this.awaitingAuthAck = false;
        this.onAuthenticated();
        return;
      }
      this.logger.debug('received', msg.type);
      const eventHandlers = this.handlers.get(msg.type);
      if (eventHandlers) {
        for (const handler of eventHandlers) {
          handler(msg.payload, msg.actor_id, msg.actor_type);
        }
      }
      for (const handler of this.anyHandlers) {
        handler(msg);
      }
    };

    this.ws.onclose = () => {
      if (this.suppressReconnectOnNextClose) {
        this.suppressReconnectOnNextClose = false;
        return;
      }
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      // Suppress — onclose handles reconnect; errors during StrictMode
      // double-fire are expected in dev and harmless.
    };
  }

  private scheduleReconnect() {
    this.reconnectAttempt++;

    if (this.reconnectAttempt >= DEGRADED_AFTER_ATTEMPTS) {
      this.setState('degraded');
      const jitter = DEGRADED_RECONNECT_DELAY_MS * 0.2 * (Math.random() * 2 - 1);
      const delay = Math.round(DEGRADED_RECONNECT_DELAY_MS + jitter);
      this.logger.warn(
        `ws: still disconnected after ${this.reconnectAttempt} attempts, ` +
          `entering degraded mode (retrying in ${delay}ms)`,
      );
      this.reconnectTimer = setTimeout(() => this.connect(), delay);
      return;
    }

    this.setState('connecting');
    const base = Math.min(
      RECONNECT_BASE_DELAY_MS * 2 ** (this.reconnectAttempt - 1),
      RECONNECT_MAX_DELAY_MS,
    );
    const jitter = base * 0.2 * (Math.random() * 2 - 1);
    const delay = Math.round(Math.min(base + jitter, RECONNECT_MAX_DELAY_MS));

    this.logger.warn(
      `ws: disconnected, reconnecting in ${delay}ms (attempt ${this.reconnectAttempt})`,
    );
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private setState(next: WSConnectionState) {
    if (this.state === next) return;
    this.state = next;
    for (const cb of this.stateChangeCallbacks) {
      try {
        cb(next);
      } catch {
        // ignore state-change callback errors
      }
    }
  }

  private handleAuthError(reason?: string) {
    this.logger.warn(
      reason
        ? `ws: auth rejected, not reconnecting with the same token (${reason})`
        : 'ws: auth rejected, not reconnecting with the same token',
    );
    this.setState('unauthorized');
    this.suppressReconnectOnNextClose = true;
    for (const cb of this.onUnauthorizedCallbacks) {
      try {
        cb(reason);
      } catch {
        // ignore unauthorized callback errors
      }
    }
  }

  private onAuthenticated() {
    this.logger.info('connected');
    this.reconnectAttempt = 0;
    this.awaitingAuthAck = false;
    this.setState('connected');
    if (this.hasConnectedBefore) {
      for (const cb of this.onReconnectCallbacks) {
        try {
          cb();
        } catch {
          // ignore reconnect callback errors
        }
      }
    }
    this.hasConnectedBefore = true;
  }

  getState(): WSConnectionState {
    return this.state;
  }

  onStateChange(callback: (state: WSConnectionState) => void) {
    this.stateChangeCallbacks.add(callback);
    return () => {
      this.stateChangeCallbacks.delete(callback);
    };
  }

  onUnauthorized(callback: (reason?: string) => void) {
    this.onUnauthorizedCallbacks.add(callback);
    return () => {
      this.onUnauthorizedCallbacks.delete(callback);
    };
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
    this.hasConnectedBefore = false;
    this.reconnectAttempt = 0;
    this.awaitingAuthAck = false;
    this.suppressReconnectOnNextClose = false;
    this.handlers.clear();
    this.anyHandlers.clear();
    this.onReconnectCallbacks.clear();
    this.stateChangeCallbacks.clear();
    this.onUnauthorizedCallbacks.clear();
  }

  on(event: WSEventType, handler: EventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set());
    }
    this.handlers.get(event)!.add(handler);
    return () => {
      this.handlers.get(event)?.delete(handler);
    };
  }

  onAny(handler: (msg: WSMessage) => void) {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  onReconnect(callback: () => void) {
    this.onReconnectCallbacks.add(callback);
    return () => {
      this.onReconnectCallbacks.delete(callback);
    };
  }

  send(message: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }
}
