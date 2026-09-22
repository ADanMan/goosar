import type { WorkspaceSampleTask } from '@goosar/core/types';
import { serviceIdentityByPackageName } from '../onboarding/presets';
import type { HelperMcpPresetName } from '../onboarding/presets';
import { isPersonalActionable, type ServiceCredentialStatus } from './credential-status';

export function sampleTaskBlockers(
  task: Pick<WorkspaceSampleTask, 'requires'>,
  statuses: readonly ServiceCredentialStatus[],
): HelperMcpPresetName[] {
  const blockers: HelperMcpPresetName[] = [];
  for (const packageName of task.requires) {
    const identity = serviceIdentityByPackageName(packageName);
    if (!identity) continue;
    const status = statuses.find((s) => s.preset === identity.preset);
    if (!status) continue;
    if (!isPersonalActionable(status)) continue;
    if (!blockers.includes(identity.preset)) blockers.push(identity.preset);
  }
  return blockers;
}
