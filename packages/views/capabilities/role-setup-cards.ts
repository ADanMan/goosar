// Что ещё нужно только что присоединённой роли, прежде чем она сможет работать:
// карточки настройки для пустого воркспейса роли.
export type RoleSetupCard = { kind: 'autopilots'; count: number } | { kind: 'runtime' };

export function roleSetupCards({
  ready,
  pausedAutopilotCount,
  hasPublicRuntime,
}: {
  ready: boolean;
  pausedAutopilotCount: number;
  hasPublicRuntime: boolean;
}): RoleSetupCard[] {
  if (!ready) return [];
  const cards: RoleSetupCard[] = [];
  if (pausedAutopilotCount > 0) {
    cards.push({ kind: 'autopilots', count: pausedAutopilotCount });
  }
  if (!hasPublicRuntime) cards.push({ kind: 'runtime' });
  return cards;
}
