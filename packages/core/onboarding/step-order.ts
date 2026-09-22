import type { OnboardingStep } from './types';

export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  'about_you',
  'workspace',
  'runtime',
  'work_tools',
] as const;
