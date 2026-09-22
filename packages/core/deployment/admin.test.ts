import { describe, it, expect, vi } from 'vitest';

vi.mock('../api', () => ({ api: {} }));

import { deploymentFleetOptions, DEPLOYMENT_FLEET_REFRESH_MS } from './admin';

describe('deploymentFleetOptions (#398)', () => {
  it('polls only while the document is visible', () => {
    const options = deploymentFleetOptions();
    expect(options.refetchInterval).toBe(DEPLOYMENT_FLEET_REFRESH_MS);
    expect(
      (options as { refetchIntervalInBackground?: boolean }).refetchIntervalInBackground,
    ).toBeUndefined();
  });
});
