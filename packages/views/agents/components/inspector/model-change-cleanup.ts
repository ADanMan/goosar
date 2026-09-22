import type { RuntimeModel } from '@goosar/core/types';

export type ModelCatalog = readonly RuntimeModel[] | null;

export type ModelChangeUpdate = {
  model: string;
  thinking_level?: string;
  service_tier?: string;
};

export function buildModelChangeUpdate(input: {
  model: string;
  thinkingLevel: string;
  serviceTier: string;
  catalog: ModelCatalog;
}): ModelChangeUpdate {
  const update: ModelChangeUpdate = { model: input.model };
  if (!input.thinkingLevel && !input.serviceTier) return update;
  if (input.catalog === null || !input.model) return update;

  const entry = input.catalog.find((model) => model.id === input.model);
  if (!entry) return update;

  const supportsThinking = (entry.thinking?.supported_levels ?? []).some(
    (level) => level.value === input.thinkingLevel,
  );
  if (input.thinkingLevel && !supportsThinking) update.thinking_level = '';

  const supportsTier = (entry.service_tiers ?? []).some((tier) => tier.id === input.serviceTier);
  if (input.serviceTier && !supportsTier) update.service_tier = '';

  return update;
}
