import type { AgentRuntime } from '../types';

const HERMES_PROVIDER = 'runtime-j';

export const ONBOARDING_HERMES_ONLY: boolean = true;

export function filterRuntimesForOnboarding(runtimes: AgentRuntime[]): AgentRuntime[] {
  if (!ONBOARDING_HERMES_ONLY) return runtimes;
  return runtimes.filter((runtime) => runtime.provider === HERMES_PROVIDER);
}

export function sortRuntimesForOnboarding(runtimes: AgentRuntime[]): AgentRuntime[] {
  const isHermes = (runtime: AgentRuntime): boolean => runtime.provider === HERMES_PROVIDER;
  return [...runtimes.filter(isHermes), ...runtimes.filter((runtime) => !isHermes(runtime))];
}
