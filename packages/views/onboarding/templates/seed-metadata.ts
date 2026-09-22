// Метки идентичности засеянных задач-гайдов: по ним автозакрытие узнаёт
// свои задачи.
export const ONBOARDING_SEED_METADATA_KEY = 'onboarding_seed';

export const ONBOARDING_SEED = {
  installRuntime: 'install_runtime',
  createAgentGuide: 'create_agent_guide',
} as const;

export type OnboardingSeedSlug = (typeof ONBOARDING_SEED)[keyof typeof ONBOARDING_SEED];
