import { cache } from 'react';
import { cookies, headers } from 'next/headers';
import { LOCALE_COOKIE, type SupportedLocale } from '@goosar/core/i18n';
import { isSupportedLocale, GOOSAR_LOCALE_HEADER, resolveLocaleFromCookie } from './locale-routing';

export const getRequestLocale = cache(async (): Promise<SupportedLocale> => {
  const headerList = await headers();
  const headerLocale = headerList.get(GOOSAR_LOCALE_HEADER);
  if (isSupportedLocale(headerLocale)) return headerLocale;

  const cookieStore = await cookies();
  return resolveLocaleFromCookie(cookieStore.get(LOCALE_COOKIE)?.value);
});
