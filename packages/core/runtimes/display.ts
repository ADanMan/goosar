import type { AgentRuntime } from '../types';

export function runtimeDisplayName(runtime: Pick<AgentRuntime, 'name' | 'custom_name'>): string {
  const custom = runtime.custom_name?.trim();
  return custom ? custom : runtime.name;
}

export function runtimeDisplayLabel(
  runtime: Pick<AgentRuntime, 'name' | 'custom_name' | 'provider'>,
): string {
  const display = runtimeDisplayName(runtime);
  const hasCustom = !!runtime.custom_name?.trim();
  const provider = runtime.provider?.trim();
  if (!hasCustom || !provider) return display;
  return `${display} (${providerDisplayName(provider)})`;
}

export function providerDisplayName(provider: string): string {
  return provider
    .split('-')
    .map((segment) =>
      segment.length <= 1
        ? segment.toUpperCase()
        : segment.charAt(0).toUpperCase() + segment.slice(1),
    )
    .join(' ');
}
