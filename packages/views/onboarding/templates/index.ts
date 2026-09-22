import { matchLocale, type SupportedLocale } from '@goosar/core/i18n';

export {
  HELPER_INSTRUCTIONS,
  HELPER_DESCRIPTION,
  type HelperInstructionsLang,
} from './helper-instructions';
export {
  INSTALL_RUNTIME_ISSUE_TITLE,
  INSTALL_RUNTIME_ISSUE_BODY,
  FOLLOWUP_COMMENT_PREFIX,
} from './install-runtime-issue';
export {
  CREATE_AGENT_GUIDE_ISSUE_TITLE,
  getCreateAgentGuideBody,
} from './create-agent-guide-issue';
export {
  HELPER_STARTER_PROMPTS,
  STARTER_CARD_IDS,
  type StarterCardId,
} from './helper-starter-prompts';
export {
  buildUserContextSection,
  type UserContextLabels,
  type QuestionnaireRaw,
} from './user-context';
export {
  ONBOARDING_SEED,
  ONBOARDING_SEED_METADATA_KEY,
  type OnboardingSeedSlug,
} from './seed-metadata';

export type ContentLang = 'en' | 'zh' | 'ko' | 'ja' | 'ru';

const CONTENT_LANG_BY_LOCALE: Record<SupportedLocale, ContentLang> = {
  en: 'en',
  'zh-Hans': 'zh',
  ko: 'ko',
  ja: 'ja',
  ru: 'ru',
};

export function pickContentLang(language: string | null | undefined): ContentLang {
  return CONTENT_LANG_BY_LOCALE[matchLocale(language ? [language] : [])];
}
