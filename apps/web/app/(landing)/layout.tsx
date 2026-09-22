import { LocaleProvider } from '@/features/landing/i18n';
import { GithubStarsProvider } from '@/features/landing/utils/github-stars';
import { parseGithubStarsEnv } from '@/features/landing/utils/github-stars-env';
import { getRequestLocale } from '@/lib/request-locale';
import { PUBLIC_SITE_ORIGIN } from '@/lib/public-host';

const jsonLd = {
  '@context': 'https://schema.org',
  '@graph': [
    {
      '@type': 'Organization',
      name: 'Goosar',
      url: PUBLIC_SITE_ORIGIN,
      sameAs: ['https://github.com/adanman/goosar'],
    },
    {
      '@type': 'SoftwareApplication',
      name: 'Goosar',
      applicationCategory: 'ProjectManagement',
      operatingSystem: 'Web',
      description:
        'Agentic workplaces for business: assign a task to an agent the way you assign it to an employee — it does the work and returns a finished result.',
      offers: {
        '@type': 'Offer',
        price: '0',
        priceCurrency: 'USD',
      },
    },
  ],
};

export default async function LandingLayout({ children }: { children: React.ReactNode }) {
  const initialLocale = await getRequestLocale();
  const githubStars = parseGithubStarsEnv(process.env['GOOSAR_GITHUB_STARS']);

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <div className={`landing-light h-full overflow-x-hidden overflow-y-auto bg-white`}>
        <LocaleProvider initialLocale={initialLocale}>
          <GithubStarsProvider stars={githubStars}>{children}</GithubStarsProvider>
        </LocaleProvider>
      </div>
    </>
  );
}
