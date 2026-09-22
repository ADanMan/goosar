import { matchLocale, type SupportedLocale } from '@goosar/core/i18n';
import { useT } from './use-t';

export function useUiLocale(): SupportedLocale {
  const { i18n } = useT();
  return matchLocale(i18n.language ? [i18n.language] : []);
}
