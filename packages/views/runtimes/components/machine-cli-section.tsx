import type { AgentRuntime } from '@goosar/core/types';
import type { RuntimeMachine } from './runtime-machines';
import { UpdateSection } from './update-section';

export function machineUpdateRuntime(
  machine: RuntimeMachine,
  currentUserId: string | undefined,
  canManageAnyRuntime: boolean,
): AgentRuntime | null {
  if (machine.mode !== 'local') return null;

  const manageable = canManageAnyRuntime
    ? machine.runtimes
    : currentUserId
      ? machine.runtimes.filter((runtime) => runtime.owner_id === currentUserId)
      : [];
  return manageable.find((runtime) => runtime.status === 'online') ?? manageable[0] ?? null;
}

export function MachineCliSection({
  machine,
  currentUserId,
  canManageAnyRuntime,
}: {
  machine: RuntimeMachine;
  currentUserId: string | undefined;
  canManageAnyRuntime: boolean;
}) {
  const updateRuntime = machineUpdateRuntime(machine, currentUserId, canManageAnyRuntime);

  if (machine.mode !== 'local') {
    return machine.cliVersion ? <span className="font-mono">CLI {machine.cliVersion}</span> : null;
  }

  if (
    !updateRuntime &&
    machine.runtimes.length === 0 &&
    !machine.cliVersion &&
    !machine.launchedBy
  ) {
    return null;
  }

  return (
    <UpdateSection
      runtimeId={updateRuntime?.id ?? null}
      currentVersion={machine.cliVersion}
      isOnline={updateRuntime?.status === 'online'}
      launchedBy={machine.launchedBy}
    />
  );
}
