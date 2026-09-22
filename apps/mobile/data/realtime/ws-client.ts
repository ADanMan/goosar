// Мобильный WebSocket-транспорт (первый слой): один сокет, переподключение
// с backoff, диспетчеризация кадров. Ничего не знает о React и бизнес-событиях.
import type {
  WSEventPayload,
  WSEventType,
  WSMessage,
} from "@goosar/core/types";

type EventHandler = (payload: unknown, actorId?: string) => void;
type AnyHandler = (msg: WSMessage) => void;

interface Logger {
  info: (...args: unknown[]) => void;
  warn: (...args: unknown[]) => void;
  debug: (...args: unknown[]) => void;
}

const noopLogger: Logger = {
  info: () => {},
  warn: () => {},
  debug: () => {},
};

export interface WSClientOptions {
  url: string;
  token: string;
  workspaceSlug: string;
  clientVersion?: string;
  logger?: Logger;
}

const RECONNECT_BASE_MS = 1_000;
const RECONNECT_CAP_MS = 30_000;
const RECONNECT_MAX_EXPONENT = 6; 

type State = "idle" | "active" | "paused";

export class WSClient {
  private ws: WebSocket | null = null;
  private state: State = "idle";
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private hasConnectedBefore = false;

  private readonly handlers = new Map<WSEventType, Set<EventHandler>>();
  private readonly anyHandlers = new Set<AnyHandler>();
  private readonly onReconnectCallbacks = new Set<() => void>();

  private readonly opts: WSClientOptions;
  private readonly logger: Logger;

  constructor(opts: WSClientOptions) {
    this.opts = opts;
    this.logger = opts.logger ?? noopLogger;
  }

  connect() {
    if (this.state === "active") return;
    this.state = "active";
    this.openSocket();
  }

  disconnect() {
    this.state = "idle";
    this.clearReconnect();
    this.teardownSocket();
    this.reconnectAttempt = 0;
    this.hasConnectedBefore = false;
  }

  pause() {
    if (this.state !== "active") return;
    this.state = "paused";
    this.clearReconnect();
    this.teardownSocket();
  }

  resume() {
    if (this.state !== "paused") return;
    this.state = "active";
    this.reconnectAttempt = 0;
    this.openSocket();
  }

  forceReconnect() {
    if (this.state !== "active") return;
    this.clearReconnect();
    this.teardownSocket();
    this.reconnectAttempt = 0;
    this.openSocket();
  }

  on<E extends WSEventType>(
    event: E,
    handler: (payload: WSEventPayload<E>, actorId?: string) => void,
  ) {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    set.add(handler as EventHandler);
    return () => {
      this.handlers.get(event)?.delete(handler as EventHandler);
    };
  }

  onAny(handler: AnyHandler) {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  onReconnect(cb: () => void) {
    this.onReconnectCallbacks.add(cb);
    return () => {
      this.onReconnectCallbacks.delete(cb);
    };
  }

  send(message: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }

  private openSocket() {
    const url = new URL(this.opts.url);
    url.searchParams.set("workspace_slug", this.opts.workspaceSlug);
    url.searchParams.set("client_platform", "mobile");
    url.searchParams.set("client_os", "ios");
    if (this.opts.clientVersion) {
      url.searchParams.set("client_version", this.opts.clientVersion);
    }

    const ws = new WebSocket(url.toString());
    this.ws = ws;
    this.logger.info("[ws] dialing", url.toString().replace(/token=[^&]*/, "token=…"));

    ws.onopen = () => {
      this.logger.info("[ws] socket open, sending auth frame");
      ws.send(
        JSON.stringify({ type: "auth", payload: { token: this.opts.token } }),
      );
    };

    ws.onmessage = (event) => {
      let msg: WSMessage;
      try {
        msg = JSON.parse(event.data as string) as WSMessage;
      } catch {
        this.logger.warn("[ws] non-JSON frame ignored");
        return;
      }

      const type = (msg as { type?: string }).type;
      if (type === "auth_ack") {
        this.onAuthenticated();
        return;
      }
      if (!type) {
        this.logger.warn("[ws] frame without type", event.data);
        return;
      }

      this.logger.debug("[ws] event", type);
      const set = this.handlers.get(msg.type);
      if (set) {
        for (const handler of set) handler(msg.payload, msg.actor_id);
      }
      for (const handler of this.anyHandlers) handler(msg);
    };

    ws.onerror = () => {
      // onerror is always paired with onclose — let onclose handle
      // reconnect. Logging here adds noise during normal teardown.
    };

    ws.onclose = () => {
      const wasOurs = this.ws === ws;
      if (!wasOurs) return;

      this.ws = null;
      this.logger.warn("[ws] socket closed");
      if (this.state === "active") this.scheduleReconnect();
    };
  }

  private onAuthenticated() {
    this.reconnectAttempt = 0;
    this.logger.info("[ws] authenticated");
    if (this.hasConnectedBefore) {
      for (const cb of this.onReconnectCallbacks) {
        try {
          cb();
        } catch (err) {
          this.logger.warn("[ws] onReconnect callback threw", err);
        }
      }
    }
    this.hasConnectedBefore = true;
  }

  private scheduleReconnect() {
    this.reconnectAttempt += 1;
    const exp = Math.min(this.reconnectAttempt, RECONNECT_MAX_EXPONENT);
    const ceiling = Math.min(RECONNECT_BASE_MS * 2 ** exp, RECONNECT_CAP_MS);
    const delay = Math.floor(Math.random() * ceiling);
    this.logger.info(
      `[ws] reconnecting in ${delay}ms (attempt ${this.reconnectAttempt})`,
    );
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.state === "active") this.openSocket();
    }, delay);
  }

  private clearReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private teardownSocket() {
    if (!this.ws) return;
    const ws = this.ws;
    ws.onopen = null;
    ws.onmessage = null;
    ws.onerror = null;
    ws.onclose = null;
    try {
      ws.close();
    } catch {
      // close() can throw if the socket is already in CLOSING/CLOSED;
      // harmless.
    }
    this.ws = null;
  }
}
