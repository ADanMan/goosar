/**
 * @vitest-environment jsdom
 */
import { renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { WSConnectionState } from '../api/ws-client';

let mockConnectionState: WSConnectionState = 'connected';
vi.mock('./provider', () => ({
  useWSConnectionState: () => mockConnectionState,
}));

import {
  useRealtimeConnectionState,
  useRealtimePollingInterval,
  DEFAULT_DEGRADED_POLL_INTERVAL_MS,
} from './use-realtime-polling-interval';

describe('useRealtimeConnectionState', () => {
  it("returns the WSProvider's connectionState", () => {
    mockConnectionState = 'degraded';
    const { result } = renderHook(() => useRealtimeConnectionState());
    expect(result.current).toBe('degraded');
  });
});

describe('useRealtimePollingInterval (#114 REST polling fallback)', () => {
  it('returns false while connecting — no polling needed yet', () => {
    mockConnectionState = 'connecting';
    const { result } = renderHook(() => useRealtimePollingInterval());
    expect(result.current).toBe(false);
  });

  it('returns false while connected — WS invalidation is doing the work', () => {
    mockConnectionState = 'connected';
    const { result } = renderHook(() => useRealtimePollingInterval());
    expect(result.current).toBe(false);
  });

  it('returns the default polling interval while degraded', () => {
    mockConnectionState = 'degraded';
    const { result } = renderHook(() => useRealtimePollingInterval());
    expect(result.current).toBe(DEFAULT_DEGRADED_POLL_INTERVAL_MS);
  });

  it('honors a custom interval while degraded', () => {
    mockConnectionState = 'degraded';
    const { result } = renderHook(() => useRealtimePollingInterval(5_000));
    expect(result.current).toBe(5_000);
  });

  it('returns false while unauthorized — REST is failing too, polling would just add noise', () => {
    mockConnectionState = 'unauthorized';
    const { result } = renderHook(() => useRealtimePollingInterval());
    expect(result.current).toBe(false);
  });

  it('clears (false) again once the state moves from degraded back to connected', () => {
    mockConnectionState = 'degraded';
    const { result, rerender } = renderHook(() => useRealtimePollingInterval());
    expect(result.current).toBe(DEFAULT_DEGRADED_POLL_INTERVAL_MS);

    mockConnectionState = 'connected';
    rerender();
    expect(result.current).toBe(false);
  });
});
