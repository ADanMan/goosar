import { describe, expect, it } from 'vitest';

import { failureReasonLabel } from './task-failure';

describe('failureReasonLabel', () => {
  it('labels the refined agent_error.* reasons the backend has written since MUL-1949', () => {
    expect(failureReasonLabel('agent_error.provider_auth_or_access')).toBe('Provider auth failed');
    expect(failureReasonLabel('agent_error.provider_capacity_or_rate_limit')).toBe(
      'Rate limited by provider',
    );
    expect(failureReasonLabel('agent_error.context_overflow')).toBe('Context window exceeded');
    expect(failureReasonLabel('queued_expired')).toBe('Expired in queue');
  });

  it('still labels the pre-MUL-1949 coarse reasons on historical rows', () => {
    expect(failureReasonLabel('agent_error')).toBe('Agent execution error');
    expect(failureReasonLabel('runtime_offline')).toBe('Daemon offline');
    expect(failureReasonLabel('runtime_reconnect_timeout')).toBe(
      'Daemon did not reconnect in time',
    );
    expect(failureReasonLabel('manual')).toBe('Cancelled by user');
  });

  it('falls back to the raw wire value for a reason this build predates', () => {
    expect(failureReasonLabel('agent_error.from_a_newer_backend')).toBe(
      'agent_error.from_a_newer_backend',
    );
  });

  it('returns null when there is no reason to show', () => {
    expect(failureReasonLabel('')).toBeNull();
    expect(failureReasonLabel(null)).toBeNull();
    expect(failureReasonLabel(undefined)).toBeNull();
  });
});
