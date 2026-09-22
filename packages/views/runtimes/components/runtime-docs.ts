import { docsUrl } from '@goosar/core/i18n';

export function installRuntimeDocsHref(language?: string): string {
  return docsUrl(language, '/install-agent-runtime');
}

export function customRuntimeDocsHref(language?: string): string {
  return docsUrl(language, '/custom-runtimes');
}
