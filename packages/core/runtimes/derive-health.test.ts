import { describe, expect, it } from 'vitest';
import type { AgentRuntime } from '../types';
import { deriveRuntimeHealth, deriveRuntimeState } from './derive-health';

const FIXED_NOW = new Date('2026-04-27T12:00:00Z').getTime();

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: 'rt-1',
    workspace_id: 'ws-1',
    daemon_id: 'daemon-1',
    name: 'Test Runtime',
    runtime_mode: 'local',
    provider: 'claude',
    launch_header: '',
    status: 'online',
    device_info: '',
    metadata: {},
    owner_id: null,
    visibility: 'private',
    last_seen_at: new Date(FIXED_NOW - 10_000).toISOString(),
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    ...overrides,
  };
}

describe('deriveRuntimeHealth', () => {
  it('returns online when status is online (regardless of last_seen_at)', () => {
    expect(
      deriveRuntimeHealth(makeRuntime({ status: 'online', last_seen_at: null }), FIXED_NOW),
    ).toBe('online');
  });

  it('returns recently_lost when offline less than 5 minutes', () => {
    expect(
      deriveRuntimeHealth(
        makeRuntime({
          status: 'offline',
          last_seen_at: new Date(FIXED_NOW - 2 * 60_000).toISOString(),
        }),
        FIXED_NOW,
      ),
    ).toBe('recently_lost');
  });

  it('returns offline when offline between 5 minutes and 6 days', () => {
    expect(
      deriveRuntimeHealth(
        makeRuntime({
          status: 'offline',
          last_seen_at: new Date(FIXED_NOW - 60 * 60_000).toISOString(), // 1 hour
        }),
        FIXED_NOW,
      ),
    ).toBe('offline');
  });

  it('returns about_to_gc when offline beyond 6 days (within 1 day of GC)', () => {
    expect(
      deriveRuntimeHealth(
        makeRuntime({
          status: 'offline',
          last_seen_at: new Date(FIXED_NOW - 6.5 * 24 * 3600_000).toISOString(),
        }),
        FIXED_NOW,
      ),
    ).toBe('about_to_gc');
  });

  it('treats null last_seen_at as long-offline (about_to_gc)', () => {
    expect(
      deriveRuntimeHealth(makeRuntime({ status: 'offline', last_seen_at: null }), FIXED_NOW),
    ).toBe('about_to_gc');
  });

  it('respects the 5-minute boundary (just inside → recently_lost)', () => {
    expect(
      deriveRuntimeHealth(
        makeRuntime({
          status: 'offline',
          last_seen_at: new Date(FIXED_NOW - (5 * 60_000 - 1_000)).toISOString(),
        }),
        FIXED_NOW,
      ),
    ).toBe('recently_lost');
  });

  it('respects the 5-minute boundary (just outside → offline)', () => {
    expect(
      deriveRuntimeHealth(
        makeRuntime({
          status: 'offline',
          last_seen_at: new Date(FIXED_NOW - (5 * 60_000 + 1_000)).toISOString(),
        }),
        FIXED_NOW,
      ),
    ).toBe('offline');
  });
});

function failedCustomRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return makeRuntime({
    id: 'rt-custom-failed',
    status: 'offline',
    last_seen_at: new Date(FIXED_NOW - 1_000).toISOString(),
    metadata: {
      runtime_profile_registration_error: true,
      runtime_profile_failure_reason: 'command not found on PATH: hermes',
      command_name: 'hermes',
    },
    profile_id: 'profile-1',
    ...overrides,
  });
}

describe('deriveRuntimeState', () => {
  it('reports a registered runtime as live and carries its own last_seen_at', () => {
    const lastSeen = new Date(FIXED_NOW - 10_000).toISOString();
    const state = deriveRuntimeState(
      makeRuntime({ status: 'online', last_seen_at: lastSeen }),
      FIXED_NOW,
    );

    expect(state).toMatchObject({
      availability: 'live',
      health: 'online',
      lastSeenAt: lastSeen,
      needsAttention: false,
    });
  });

  it('flags a live runtime that stopped heartbeating as needing attention', () => {
    const state = deriveRuntimeState(
      makeRuntime({
        status: 'offline',
        last_seen_at: new Date(FIXED_NOW - 60 * 60_000).toISOString(),
      }),
      FIXED_NOW,
    );

    expect(state.availability).toBe('live');
    expect(state.health).toBe('offline');
    expect(state.needsAttention).toBe(true);
  });

  it('classifies a machine-bound custom runtime as unavailable here, not as an error', () => {
    const state = deriveRuntimeState(failedCustomRuntime(), FIXED_NOW);

    expect(state.availability).toBe('unavailable_here');
    expect(state.health).toBeNull();
    expect(state.needsAttention).toBe(false);
    expect(state.failureReason).toBe('command not found on PATH: hermes');
    expect(state.commandName).toBe('hermes');
  });

  it("never presents the server's registration stamp as a last-seen time", () => {
    const runtime = failedCustomRuntime();
    expect(runtime.last_seen_at).not.toBeNull();

    expect(deriveRuntimeState(runtime, FIXED_NOW).lastSeenAt).toBeNull();
  });

  it('still classifies as unavailable here when the daemon reports no reason', () => {
    const state = deriveRuntimeState(
      failedCustomRuntime({
        metadata: {
          runtime_profile_registration_error: true,
          command_name: 'hermes',
        },
      }),
      FIXED_NOW,
    );

    expect(state.availability).toBe('unavailable_here');
    expect(state.failureReason).toBeNull();
  });

  it('treats a client-side pending placeholder as registering, with no last-seen', () => {
    const pendingSince = new Date(FIXED_NOW - 2_000).toISOString();
    const state = deriveRuntimeState(
      makeRuntime({
        status: 'offline',
        last_seen_at: pendingSince,
        metadata: {
          pending_custom_runtime: true,
          runtime_profile_enabled: true,
          command_name: 'team-codex',
          pending_since: pendingSince,
        },
      }),
      FIXED_NOW,
    );

    expect(state.availability).toBe('registering');
    expect(state.health).toBeNull();
    expect(state.lastSeenAt).toBeNull();
    expect(state.needsAttention).toBe(false);
    expect(state.commandName).toBe('team-codex');
  });

  it('distinguishes a switched-off profile from one that is registering', () => {
    const state = deriveRuntimeState(
      makeRuntime({
        status: 'offline',
        metadata: {
          pending_custom_runtime: true,
          runtime_profile_enabled: false,
          command_name: 'team-codex',
        },
      }),
      FIXED_NOW,
    );

    expect(state.availability).toBe('disabled');
    expect(state.needsAttention).toBe(false);
  });

  it('ignores non-boolean truthiness on the failure flag', () => {
    const state = deriveRuntimeState(
      makeRuntime({
        status: 'online',
        metadata: { runtime_profile_registration_error: 'true' },
      }),
      FIXED_NOW,
    );

    expect(state.availability).toBe('live');
    expect(state.health).toBe('online');
  });
});
