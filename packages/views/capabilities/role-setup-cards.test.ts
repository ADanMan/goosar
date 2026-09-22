import { describe, expect, it } from 'vitest';
import { roleSetupCards } from './role-setup-cards';

const READY = {
  ready: true,
  pausedAutopilotCount: 0,
  hasPublicRuntime: true,
};

describe('roleSetupCards', () => {
  it('shows nothing while the sources are still loading', () => {
    expect(
      roleSetupCards({
        ...READY,
        ready: false,
        pausedAutopilotCount: 8,
        hasPublicRuntime: false,
      }),
    ).toEqual([]);
  });

  it('shows both cards for a freshly joined role', () => {
    expect(
      roleSetupCards({
        ...READY,
        pausedAutopilotCount: 8,
        hasPublicRuntime: false,
      }),
    ).toEqual([{ kind: 'autopilots', count: 8 }, { kind: 'runtime' }]);
  });

  it('drops the runtime card once a public runtime exists', () => {
    expect(
      roleSetupCards({
        ...READY,
        pausedAutopilotCount: 8,
        hasPublicRuntime: true,
      }),
    ).toEqual([{ kind: 'autopilots', count: 8 }]);
  });

  it('drops the autopilot card once none are paused', () => {
    expect(roleSetupCards({ ...READY, pausedAutopilotCount: 0 })).toEqual([]);
  });

  it('asks for a machine while no public runtime exists', () => {
    expect(roleSetupCards({ ...READY, hasPublicRuntime: false })).toEqual([{ kind: 'runtime' }]);
  });
});
