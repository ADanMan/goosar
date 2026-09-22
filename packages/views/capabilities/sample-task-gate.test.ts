import { describe, expect, it } from 'vitest';
import type { ServiceCredentialStatus } from './credential-status';
import { sampleTaskBlockers } from './sample-task-gate';

function status(
  preset: ServiceCredentialStatus['preset'],
  admin: ServiceCredentialStatus['admin'],
  personal: ServiceCredentialStatus['personal'],
): ServiceCredentialStatus {
  return { preset, admin, personal };
}

describe('sampleTaskBlockers', () => {
  it('passes a task that needs nothing', () => {
    expect(sampleTaskBlockers({ requires: [] }, [])).toEqual([]);
  });

  it('blocks on a required service whose personal key is provably missing', () => {
    const blockers = sampleTaskBlockers({ requires: ['ews-mcp'] }, [
      status('outlook', 'provided', 'missing'),
    ]);
    expect(blockers).toEqual(['outlook']);
  });

  it('passes once the member has filled their half', () => {
    expect(
      sampleTaskBlockers({ requires: ['ews-mcp'] }, [status('outlook', 'provided', 'filled')]),
    ).toEqual([]);
  });

  it('does not block on a service an administrator switched off — a key would not rescue it', () => {
    expect(
      sampleTaskBlockers({ requires: ['ews-mcp'] }, [status('outlook', 'off', 'missing')]),
    ).toEqual([]);
  });

  it('does not block on an unreadable credential picture', () => {
    expect(
      sampleTaskBlockers({ requires: ['ews-mcp'] }, [status('outlook', 'unknown', 'unknown')]),
    ).toEqual([]);
  });

  it('does not block on a package this build has no card for', () => {
    expect(sampleTaskBlockers({ requires: ['some-future-mcp'] }, [])).toEqual([]);
  });

  it('does not block when the service is simply absent from the picture', () => {
    expect(
      sampleTaskBlockers({ requires: ['b24-agent'] }, [status('outlook', 'provided', 'missing')]),
    ).toEqual([]);
  });

  it('reports every blocked service once, in the order the task names them', () => {
    const blockers = sampleTaskBlockers({ requires: ['b24-agent', 'ews-mcp', 'b24-agent'] }, [
      status('outlook', 'provided', 'missing'),
      status('bitrix24', 'provided', 'missing'),
    ]);
    expect(blockers).toEqual(['bitrix24', 'outlook']);
  });
});
