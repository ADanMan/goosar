import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { WSClient } from './ws-client';
import type { WSMessage } from '../types/events';

class FakeWebSocket {
  static lastUrl: string | null = null;
  static lastInstance: FakeWebSocket | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  constructor(url: string) {
    FakeWebSocket.lastUrl = url;
    FakeWebSocket.lastInstance = this;
  }
  close() {}
  send() {}
}

describe('WSClient', () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    FakeWebSocket.lastInstance = null;
    vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('includes client identity in the upgrade URL when configured', () => {
    const ws = new WSClient('ws://example.test/ws', {
      identity: { platform: 'desktop', version: '1.2.3', os: 'macos' },
    });
    ws.setAuth('tok', 'acme');
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.get('workspace_slug')).toBe('acme');
    expect(url.searchParams.get('client_platform')).toBe('desktop');
    expect(url.searchParams.get('client_version')).toBe('1.2.3');
    expect(url.searchParams.get('client_os')).toBe('macos');
    expect(url.searchParams.has('token')).toBe(false);
  });

  it('omits client_* params when identity is not configured', () => {
    const ws = new WSClient('ws://example.test/ws');
    ws.setAuth('tok', 'acme');
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.has('client_platform')).toBe(false);
    expect(url.searchParams.has('client_version')).toBe(false);
    expect(url.searchParams.has('client_os')).toBe(false);
  });

  it('only includes the identity fields that are set', () => {
    const ws = new WSClient('ws://example.test/ws', {
      identity: { platform: 'cli' },
    });
    ws.setAuth('tok', 'acme');
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.get('client_platform')).toBe('cli');
    expect(url.searchParams.has('client_version')).toBe(false);
    expect(url.searchParams.has('client_os')).toBe(false);
  });

  it('truncates the logged payload when an unparseable frame is large', () => {
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient('ws://example.test/ws', { logger });
    ws.connect();

    const huge = 'x'.repeat(5000);
    FakeWebSocket.lastInstance!.onmessage?.({ data: huge });

    expect(logger.warn).toHaveBeenCalledTimes(1);
    const [, summary] = logger.warn.mock.calls[0] as [string, string];
    expect(summary.length).toBeLessThan(huge.length);
    expect(summary).toContain('truncated');
    expect(summary).toContain('5000');
    expect(summary.startsWith('x'.repeat(200))).toBe(true);
  });

  it('logs and skips malformed frames without breaking later messages', () => {
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient('ws://example.test/ws', { logger });
    const handler = vi.fn();
    ws.on('issue:updated', handler);
    ws.connect();

    expect(() => {
      FakeWebSocket.lastInstance!.onmessage?.({ data: `{"type":"issue` });
    }).not.toThrow();

    FakeWebSocket.lastInstance!.onmessage?.({
      data: JSON.stringify({
        type: 'issue:updated',
        payload: { id: 'issue-1' },
      }),
    });

    expect(logger.warn).toHaveBeenCalledWith('ws: received unparseable message', `{"type":"issue`);
    expect(handler).toHaveBeenCalledWith({ id: 'issue-1' }, undefined, undefined);
  });

  it('drops frames without a string type without throwing, and keeps dispatching', () => {
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient('ws://example.test/ws', { logger });

    const anyHandler = vi.fn((msg: WSMessage) => msg.type.split(':')[0]);
    ws.onAny(anyHandler);
    const issueHandler = vi.fn();
    ws.on('issue:updated', issueHandler);
    ws.connect();

    const badFrames = [
      JSON.stringify({ payload: {} }), // object, no type
      '42', // bare number
      'true', // bare bool
      '[]', // array
    ];
    for (const data of badFrames) {
      expect(() => {
        FakeWebSocket.lastInstance!.onmessage?.({ data });
      }).not.toThrow();
    }

    expect(anyHandler).not.toHaveBeenCalled();
    expect(issueHandler).not.toHaveBeenCalled();

    FakeWebSocket.lastInstance!.onmessage?.({
      data: JSON.stringify({ type: 'issue:updated', payload: { id: 'i-1' } }),
    });
    expect(issueHandler).toHaveBeenCalledWith({ id: 'i-1' }, undefined, undefined);
    expect(anyHandler).toHaveBeenCalledTimes(1);

    expect(logger.warn).toHaveBeenCalledTimes(1);
    expect(logger.warn.mock.calls[0]?.[0]).toBe('ws: dropping frame without a string type');
  });

  it('passes actor_id and actor_type to event handlers', () => {
    const ws = new WSClient('ws://example.test/ws');
    ws.setAuth('tok', 'acme');
    ws.connect();

    const handler = vi.fn();
    ws.on('issue:created', handler);

    const fakeWs = (ws as any).ws as FakeWebSocket;
    fakeWs.onmessage?.({
      data: JSON.stringify({
        type: 'issue:created',
        payload: { id: 'issue-1' },
        actor_id: 'user-123',
        actor_type: 'user',
      }),
    });

    expect(handler).toHaveBeenCalledWith({ id: 'issue-1' }, 'user-123', 'user');
  });

  describe('reconnect backoff', () => {
    let setTimeoutSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      vi.useFakeTimers();
      setTimeoutSpy = vi.spyOn(globalThis, 'setTimeout');
    });

    afterEach(() => {
      setTimeoutSpy.mockRestore();
      vi.useRealTimers();
    });

    function lastTimerDelay(): number {
      const calls = setTimeoutSpy.mock.calls;
      return calls[calls.length - 1]?.[1] as number;
    }

    function simulateDisconnect() {
      FakeWebSocket.lastInstance!.onclose?.();
    }

    function simulateAuthAck() {
      FakeWebSocket.lastInstance!.onmessage?.({
        data: JSON.stringify({ type: 'auth_ack' }),
      });
    }

    it('uses ~1000ms base delay for the first reconnect attempt', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      ws.connect();
      simulateDisconnect();

      expect(lastTimerDelay()).toBe(1000);
    });

    it('doubles the base delay on consecutive failures (exponential)', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      ws.connect();

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);

      vi.advanceTimersByTime(1000);
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(2000);

      vi.advanceTimersByTime(2000);
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(4000);

      vi.advanceTimersByTime(4000);
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(8000);
    });

    it('caps the delay at 30s during the exponential phase, then enters degraded mode', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      ws.connect();

      const delays = [1000, 2000, 4000, 8000, 16000, 30000, 30000];
      for (const d of delays) {
        simulateDisconnect();
        expect(lastTimerDelay()).toBe(d);
        expect(ws.getState()).toBe('connecting');
        vi.advanceTimersByTime(d);
      }

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(120_000);
      expect(ws.getState()).toBe('degraded');
    });

    it('applies jitter so delays vary with Math.random', () => {
      let callCount = 0;
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => (callCount++ % 2 === 0 ? 0 : 1);
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');

      ws.connect();
      simulateDisconnect();
      const delay1 = lastTimerDelay();

      vi.clearAllTimers();
      ws.disconnect();
      ws.connect();
      simulateDisconnect();
      const delay2 = lastTimerDelay();

      expect(delay1).toBe(800);
      expect(delay2).toBe(1200);
    });

    it('resets the attempt counter on successful authentication', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('tok', 'acme');
      ws.connect();

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
      vi.advanceTimersByTime(1000);

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(2000);
      vi.advanceTimersByTime(2000);

      simulateAuthAck();

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
    });

    it('keeps retrying indefinitely at a bounded, degraded cadence once past the attempt threshold', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const logger = {
        debug: vi.fn(),
        info: vi.fn(),
        warn: vi.fn(),
        error: vi.fn(),
      };
      const ws = new WSClient('ws://example.test/ws', { logger });
      ws.connect();

      for (let i = 0; i < 8; i++) {
        simulateDisconnect();
        vi.advanceTimersByTime(lastTimerDelay());
      }
      expect(ws.getState()).toBe('degraded');

      for (let i = 0; i < 20; i++) {
        const timerCountBefore = setTimeoutSpy.mock.calls.length;
        simulateDisconnect();
        expect(setTimeoutSpy.mock.calls.length).toBe(timerCountBefore + 1);
        expect(lastTimerDelay()).toBe(120_000);
        expect(ws.getState()).toBe('degraded');
        vi.advanceTimersByTime(120_000);
      }
      expect(logger.error).not.toHaveBeenCalled();
    });

    it('notifies onStateChange subscribers as it moves connecting -> degraded', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      const states: string[] = [];
      const unsub = ws.onStateChange((s) => states.push(s));
      expect(ws.getState()).toBe('connecting');

      ws.connect();
      for (let i = 0; i < 8; i++) {
        simulateDisconnect();
        vi.advanceTimersByTime(lastTimerDelay());
      }

      expect(states).toEqual(['degraded']);
      expect(ws.getState()).toBe('degraded');

      unsub();
    });

    it('disconnect() cancels a pending reconnect and resets the counter', () => {
      vi.stubGlobal(
        'Math',
        new Proxy(Math, {
          get(target, prop) {
            if (prop === 'random') return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient('ws://example.test/ws');
      ws.connect();

      simulateDisconnect();
      vi.advanceTimersByTime(1000);
      simulateDisconnect();

      ws.disconnect();
      vi.advanceTimersByTime(10_000);

      ws.connect();
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
    });
  });

  describe('auth rejection', () => {
    let setTimeoutSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      vi.useFakeTimers();
      setTimeoutSpy = vi.spyOn(globalThis, 'setTimeout');
    });

    afterEach(() => {
      setTimeoutSpy.mockRestore();
      vi.useRealTimers();
    });

    function simulateOpen() {
      FakeWebSocket.lastInstance!.onopen?.();
    }

    function simulateServerMessage(data: unknown) {
      FakeWebSocket.lastInstance!.onmessage?.({ data: JSON.stringify(data) });
    }

    function simulateDisconnect() {
      FakeWebSocket.lastInstance!.onclose?.();
    }

    it("transitions to 'unauthorized' on a tagged {type:auth_error} frame", () => {
      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('bad-token', 'acme');
      ws.connect();
      simulateOpen();

      simulateServerMessage({ type: 'auth_error', error: 'invalid token' });

      expect(ws.getState()).toBe('unauthorized');
    });

    it("also recognizes the server's current untagged {error} frame while awaiting auth_ack", () => {
      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('bad-token', 'acme');
      ws.connect();
      simulateOpen();

      simulateServerMessage({ error: 'invalid token' });

      expect(ws.getState()).toBe('unauthorized');
    });

    it('does not reconnect with the same rejected token when the server closes after auth_error', () => {
      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('bad-token', 'acme');
      ws.connect();
      simulateOpen();
      simulateServerMessage({ type: 'auth_error', error: 'invalid token' });

      const timerCountBefore = setTimeoutSpy.mock.calls.length;
      simulateDisconnect(); 

      expect(setTimeoutSpy.mock.calls.length).toBe(timerCountBefore);
      expect(ws.getState()).toBe('unauthorized');

      vi.advanceTimersByTime(5 * 60_000);
      expect(setTimeoutSpy.mock.calls.length).toBe(timerCountBefore);
    });

    it('invokes onUnauthorized callbacks with the rejection reason', () => {
      const ws = new WSClient('ws://example.test/ws');
      const cb = vi.fn();
      ws.onUnauthorized(cb);
      ws.setAuth('bad-token', 'acme');
      ws.connect();
      simulateOpen();

      simulateServerMessage({ type: 'auth_error', error: 'invalid token' });

      expect(cb).toHaveBeenCalledWith('invalid token');
    });

    it('is distinct from a transport failure: a normal auth_ack still authenticates', () => {
      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('good-token', 'acme');
      ws.connect();
      simulateOpen();

      simulateServerMessage({ type: 'auth_ack' });

      expect(ws.getState()).toBe('connected');
    });

    it('does not misinterpret an unrelated error-shaped frame once already authenticated', () => {
      const ws = new WSClient('ws://example.test/ws');
      ws.setAuth('good-token', 'acme');
      ws.connect();
      simulateOpen();
      simulateServerMessage({ type: 'auth_ack' });
      expect(ws.getState()).toBe('connected');

      simulateServerMessage({ type: 'issue:updated', error: 'unrelated', payload: {} });

      expect(ws.getState()).toBe('connected');
    });
  });
});
