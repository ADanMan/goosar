import type { MetadataRoute } from 'next';

import { PUBLIC_SITE_ORIGIN } from '@/lib/public-host';

export default function sitemap(): MetadataRoute.Sitemap {
  const baseUrl = PUBLIC_SITE_ORIGIN;

  return [
    {
      url: baseUrl,
      lastModified: new Date('2026-04-01'),
      changeFrequency: 'weekly',
      priority: 1.0,
    },
    {
      url: `${baseUrl}/about`,
      lastModified: new Date('2026-04-01'),
      changeFrequency: 'monthly',
      priority: 0.7,
    },
  ];
}
