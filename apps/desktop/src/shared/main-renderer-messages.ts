// Сообщения главного процесса, которые должны дождаться, пока главный рендерер
// установит слушателя: завершения loadURL недостаточно, React-эффекты
// подписываются позже.
export const MAIN_RENDERER_CHANNEL_STATE_CHANNEL = 'main-renderer:channel-state';

export const MAIN_RENDERER_MESSAGE_CHANNELS = [
  'auth:token',
  'auth:link-token',
  'auth:mfa-token',
  'auth:error',
  'invite:open',
  'inbox:open',
] as const;

export type MainRendererMessageChannel = (typeof MAIN_RENDERER_MESSAGE_CHANNELS)[number];

export interface MainRendererChannelState {
  channel: MainRendererMessageChannel;
  ready: boolean;
}

const mainRendererMessageChannels = new Set<string>(MAIN_RENDERER_MESSAGE_CHANNELS);

export function parseMainRendererChannelState(value: unknown): MainRendererChannelState | null {
  if (!value || typeof value !== 'object') return null;
  const candidate = value as Record<string, unknown>;
  if (
    typeof candidate.channel !== 'string' ||
    !mainRendererMessageChannels.has(candidate.channel) ||
    typeof candidate.ready !== 'boolean'
  ) {
    return null;
  }
  return {
    channel: candidate.channel as MainRendererMessageChannel,
    ready: candidate.ready,
  };
}

type SendMessage = (channel: MainRendererMessageChannel, payload: unknown) => void;

export class MainRendererMessageQueue {
  private readonly readyChannels = new Set<MainRendererMessageChannel>();
  private readonly pending = new Map<MainRendererMessageChannel, unknown[]>();

  enqueue(channel: MainRendererMessageChannel, payload: unknown, send: SendMessage): void {
    if (this.readyChannels.has(channel)) {
      send(channel, payload);
      return;
    }
    const queued = this.pending.get(channel) ?? [];
    queued.push(payload);
    this.pending.set(channel, queued);
  }

  setReady(channel: MainRendererMessageChannel, ready: boolean, send: SendMessage): void {
    if (!ready) {
      this.readyChannels.delete(channel);
      return;
    }

    this.readyChannels.add(channel);
    const queued = this.pending.get(channel);
    if (!queued) return;
    this.pending.delete(channel);
    for (const payload of queued) send(channel, payload);
  }

  resetReady(): void {
    this.readyChannels.clear();
  }

  clear(channel: MainRendererMessageChannel): void {
    this.pending.delete(channel);
  }
}
