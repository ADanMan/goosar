'use client';

import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '../auth';
import { useLocaleAdapter } from './adapter-context';
import { SUPPORTED_LOCALES, type SupportedLocale } from './types';

export function UserLocaleSync() {
  const userLanguage = useAuthStore((s) => s.user?.language ?? null);
  const adapter = useLocaleAdapter();
  const { i18n } = useTranslation();

  useEffect(() => {
    if (!userLanguage) return;
    if (!(SUPPORTED_LOCALES as readonly string[]).includes(userLanguage)) {
      return;
    }
    adapter.persist(userLanguage as SupportedLocale);
    if (userLanguage === i18n.language) return;
    if (typeof window !== 'undefined') window.location.reload();
  }, [userLanguage, i18n.language, adapter]);

  return null;
}
