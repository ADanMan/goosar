import { describe, expect, it } from 'vitest';
import type { AgentRuntime } from '../types';
import {
  filterRuntimesForOnboarding,
  ONBOARDING_HERMES_ONLY,
  sortRuntimesForOnboarding,
} from './ordering';

function makeRuntime(
  id: string,
  provider: string,
  overrides: Partial<AgentRuntime> = {},
): AgentRuntime {
  return {
    id,
    workspace_id: 'ws-1',
    daemon_id: 'daemon-1',
    name: `Runtime ${id}`,
    runtime_mode: 'local',
    provider,
    launch_header: '',
    status: 'online',
    device_info: '',
    metadata: {},
    owner_id: null,
    visibility: 'private',
    last_seen_at: '2026-04-27T12:00:00Z',
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    ...overrides,
  };
}

const ids = (runtimes: AgentRuntime[]): string[] => runtimes.map((r) => r.id);

describe('sortRuntimesForOnboarding', () => {
  it('puts hermes runtimes first', () => {
    const sorted = sortRuntimesForOnboarding([
      makeRuntime('claude-1', 'claude'),
      makeRuntime('bee-1', 'runtime-j'),
      makeRuntime('codex-1', 'codex'),
    ]);

    expect(ids(sorted)).toEqual(['bee-1', 'claude-1', 'codex-1']);
  });

  it('keeps every non-hermes runtime in the list', () => {
    const input = [
      makeRuntime('claude-1', 'claude'),
      makeRuntime('bee-1', 'runtime-j'),
      makeRuntime('codex-1', 'codex'),
    ];

    expect(sortRuntimesForOnboarding(input)).toHaveLength(input.length);
  });

  it('preserves the server order within each group', () => {
    const sorted = sortRuntimesForOnboarding([
      makeRuntime('codex-1', 'codex'),
      makeRuntime('bee-1', 'runtime-j'),
      makeRuntime('claude-1', 'claude'),
      makeRuntime('bee-2', 'runtime-j'),
      makeRuntime('codex-2', 'codex'),
    ]);

    expect(ids(sorted)).toEqual(['bee-1', 'bee-2', 'codex-1', 'claude-1', 'codex-2']);
  });

  it('returns the server order unchanged when no runtime is hermes', () => {
    const sorted = sortRuntimesForOnboarding([
      makeRuntime('codex-1', 'codex'),
      makeRuntime('claude-1', 'claude'),
    ]);

    expect(ids(sorted)).toEqual(['codex-1', 'claude-1']);
  });

  it('returns the runtimes unchanged when every runtime is hermes', () => {
    const sorted = sortRuntimesForOnboarding([
      makeRuntime('bee-1', 'runtime-j'),
      makeRuntime('bee-2', 'runtime-j'),
    ]);

    expect(ids(sorted)).toEqual(['bee-1', 'bee-2']);
  });

  it('handles an empty list', () => {
    expect(sortRuntimesForOnboarding([])).toEqual([]);
  });

  it('does not mutate the input array', () => {
    const input = [makeRuntime('claude-1', 'claude'), makeRuntime('bee-1', 'runtime-j')];
    const snapshot = [...input];

    const sorted = sortRuntimesForOnboarding(input);

    expect(input).toEqual(snapshot);
    expect(ids(input)).toEqual(['claude-1', 'bee-1']);
    expect(sorted).not.toBe(input);
  });

  it('only matches the exact hermes provider slug', () => {
    const sorted = sortRuntimesForOnboarding([
      makeRuntime('hermes-ish', 'runtime-j-cloud'),
      makeRuntime('bee-1', 'runtime-j'),
    ]);

    expect(ids(sorted)).toEqual(['bee-1', 'hermes-ish']);
  });
});

describe('filterRuntimesForOnboarding', () => {
  it('ships with the hermes-only gate enabled', () => {
    expect(ONBOARDING_HERMES_ONLY).toBe(true);
  });

  it('keeps only hermes runtimes', () => {
    const filtered = filterRuntimesForOnboarding([
      makeRuntime('claude-1', 'claude'),
      makeRuntime('bee-1', 'runtime-j'),
      makeRuntime('codex-1', 'codex'),
      makeRuntime('bee-2', 'runtime-j'),
    ]);

    expect(ids(filtered)).toEqual(['bee-1', 'bee-2']);
  });

  it('only matches the exact hermes provider slug', () => {
    const filtered = filterRuntimesForOnboarding([
      makeRuntime('hermes-ish', 'runtime-j-cloud'),
      makeRuntime('bee-1', 'runtime-j'),
    ]);

    expect(ids(filtered)).toEqual(['bee-1']);
  });

  it('returns an empty list when no runtime is hermes', () => {
    const filtered = filterRuntimesForOnboarding([
      makeRuntime('claude-1', 'claude'),
      makeRuntime('codex-1', 'codex'),
    ]);

    expect(filtered).toEqual([]);
  });

  it('handles an empty list', () => {
    expect(filterRuntimesForOnboarding([])).toEqual([]);
  });

  it('does not mutate the input array', () => {
    const input = [makeRuntime('claude-1', 'claude'), makeRuntime('bee-1', 'runtime-j')];
    const snapshot = [...input];

    const filtered = filterRuntimesForOnboarding(input);

    expect(input).toEqual(snapshot);
    expect(ids(input)).toEqual(['claude-1', 'bee-1']);
    expect(filtered).not.toBe(input);
  });
});
