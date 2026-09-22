// Публичная поверхность @goosar/core/feature-flags; список намеренно минимальный.

export type { Decision, EvalContext, PercentRollout, Provider, Reason, Rule } from './types';

export { FeatureFlagService } from './service';
export { StaticProvider } from './static-provider';
export { ChainProvider } from './chain-provider';
export { COMPOSIO_MCP_APPS_FLAG, RESOURCE_LABELS_FLAG } from './keys';
export { FeatureFlagsProvider, useFeatureFlagService, useFlag, useVariant } from './context';

export { bucketFor, inPercent } from './hash';
