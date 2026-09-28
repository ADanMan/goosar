import { describe, expect, it } from 'vitest';

import { failureReasonLabel } from './task-failure';
import enAgents from '../../../locales/en/agents.json';
import ruAgents from '../../../locales/ru/agents.json';

type Bundle = Record<string, unknown>;
function fakeT(bundle: Bundle) {
  return ((selector: (b: never) => string) => selector(bundle as never)) as never;
}

const enT = fakeT(enAgents);
const ruT = fakeT(ruAgents);

describe('failureReasonLabel', () => {
  it('labels the refined agent_error.* reasons the backend has written since MUL-1949', () => {
    expect(failureReasonLabel('agent_error.provider_auth_or_access', enT)).toBe(
      'Provider auth failed',
    );
    expect(failureReasonLabel('agent_error.provider_capacity_or_rate_limit', enT)).toBe(
      'Rate limited by provider',
    );
    expect(failureReasonLabel('agent_error.context_overflow', enT)).toBe('Context window exceeded');
    expect(failureReasonLabel('queued_expired', enT)).toBe('Expired in queue');
  });

  it('still labels the pre-MUL-1949 coarse reasons on historical rows', () => {
    expect(failureReasonLabel('agent_error', enT)).toBe('Agent execution error');
    expect(failureReasonLabel('runtime_offline', enT)).toBe('Daemon offline');
    expect(failureReasonLabel('runtime_reconnect_timeout', enT)).toBe(
      'Daemon did not reconnect in time',
    );
    expect(failureReasonLabel('manual', enT)).toBe('Cancelled by user');
  });

  it('falls back to the raw wire value for a reason this build predates', () => {
    expect(failureReasonLabel('agent_error.from_a_newer_backend', enT)).toBe(
      'agent_error.from_a_newer_backend',
    );
  });

  it('returns null when there is no reason to show', () => {
    expect(failureReasonLabel('', enT)).toBeNull();
    expect(failureReasonLabel(null, enT)).toBeNull();
    expect(failureReasonLabel(undefined, enT)).toBeNull();
  });

  it('translates every known reason, not just the English source strings', () => {
    const reasons = [
      'queued_expired',
      'runtime_offline',
      'runtime_reconnect_timeout',
      'runtime_recovery',
      'timeout',
      'iteration_limit',
      'agent_blocked',
      'api_invalid_request',
      'skill_bundle_unavailable',
      'agent_error.provider_auth_or_access',
      'agent_error.provider_quota_limit',
      'agent_error.provider_capacity_or_rate_limit',
      'agent_error.provider_server_error',
      'agent_error.provider_network',
      'agent_error.process_failure',
      'agent_error.empty_or_unparseable_output',
      'agent_error.agent_timeout',
      'agent_error.context_overflow',
      'agent_error.missing_config',
      'agent_error.model_not_found_or_unavailable',
      'agent_error.runtime_version_unsupported',
      'agent_error.runtime_missing_executable',
      'agent_error.unknown',
      'agent_error',
      'codex_semantic_inactivity',
      'manual',
    ];
    for (const reason of reasons) {
      const en = failureReasonLabel(reason, enT);
      const ru = failureReasonLabel(reason, ruT);
      expect(en, reason).toBeTruthy();
      expect(ru, reason).toBeTruthy();
      expect(ru, reason).not.toBe(en);
    }
  });
});
