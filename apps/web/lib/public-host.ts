// Единственный источник канонического origin маркетингового сайта.
export const PUBLIC_SITE_ORIGIN = 'https://www.goosar.ru';

const OFFICIAL_MARKETING_HOSTS = new Set(
  [new URL(PUBLIC_SITE_ORIGIN).hostname, 'goosar.ru'].map((h) => h.toLowerCase()),
);

export function isOfficialMarketingHost(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase().replace(/\.$/, '');
  return OFFICIAL_MARKETING_HOSTS.has(normalized);
}
