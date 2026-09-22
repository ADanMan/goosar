import { describe, expect, it } from 'vitest';
import enChat from './en/chat.json';
import ruChat from './ru/chat.json';
import zhChat from './zh-Hans/chat.json';
import jaChat from './ja/chat.json';
import koChat from './ko/chat.json';

const BUNDLES = {
  en: enChat,
  ru: ruChat,
  'zh-Hans': zhChat,
  ja: jaChat,
  ko: koChat,
} as const;

const DEV_SPEAK = [
  /\bPATH\b/,
  /default_agent/,
  /\.yaml\b/,
  /\bllm\.\w+/,
  /\bstderr\b/,
  /\bexit code\b/i,
];

const ALLOWED_KEYS = new Set([
  'llm_not_configured_lead',
]);

describe('chat failure copy speaks to the person in the chat', () => {
  for (const [locale, bundle] of Object.entries(BUNDLES)) {
    it(`has no config-file instructions in ${locale}`, () => {
      const failure = (bundle as { message_list: { failure: Record<string, string> } }).message_list
        .failure;
      for (const [key, text] of Object.entries(failure)) {
        if (ALLOWED_KEYS.has(key)) continue;
        for (const pattern of DEV_SPEAK) {
          expect(pattern.test(text), `${locale}.${key}: ${text}`).toBe(false);
        }
      }
    });
  }

  it('points the unresolvable-agent and missing-runtime cases at a screen', () => {
    for (const [locale, bundle] of Object.entries(BUNDLES)) {
      const failure = (bundle as { message_list: { failure: Record<string, string> } }).message_list
        .failure;
      for (const key of ['agent_unresolved', 'runtime_missing_executable']) {
        expect(failure[key], `${locale}.${key}`).toBeTruthy();
      }
    }
  });
});
