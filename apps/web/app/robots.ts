import type { MetadataRoute } from 'next';

import { PUBLIC_SITE_ORIGIN } from '@/lib/public-host';

export default function robots(): MetadataRoute.Robots {
  const baseUrl = PUBLIC_SITE_ORIGIN;

  return {
    rules: [
      {
        userAgent: '*',
        allow: ['/', '/about'],
        disallow: [
          '/api/',
          '/ws',
          '/auth/',
          '/issues',
          '/board',
          '/inbox',
          '/agents',
          '/settings',
          '/my-issues',
          '/runtimes',
          '/skills',
        ],
      },
    ],
    sitemap: [`${baseUrl}/sitemap.xml`, `${baseUrl}/docs/sitemap.xml`],
  };
}
