import type { Metadata, Viewport } from 'next';
import Script from 'next/script';
import { ThemeProvider } from '@/components/theme-provider';
import { Toaster } from '@goosar/ui/components/ui/sonner';
import { cn } from '@goosar/ui/lib/utils';
import { WebProviders } from '@/components/web-providers';
import type { SupportedLocale } from '@goosar/core/i18n';
import { RESOURCES } from '@goosar/views/locales';
import { getRequestLocale } from '@/lib/request-locale';
import { PUBLIC_SITE_ORIGIN } from '@/lib/public-host';
import { resolveBrowserApiBaseUrl, resolveBrowserWsUrl } from '@/config/runtime-urls';
import { manrope, unbounded, jetbrainsMono } from './fonts';
import './globals.css';

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#ffffff' },
    { media: '(prefers-color-scheme: dark)', color: '#1c1917' },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL(PUBLIC_SITE_ORIGIN),
  title: {
    default: 'Goosar — Project Management for Human + Agent Teams',
    template: '%s | Goosar',
  },
  description:
    'Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.',
  icons: {
    icon: [{ url: '/goosar-icon.svg', type: 'image/svg+xml' }],
    shortcut: ['/goosar-icon.svg'],
  },
  openGraph: {
    type: 'website',
    siteName: 'Goosar',
    locale: 'en_US',
  },
  twitter: {
    card: 'summary_large_image',
    site: '@goosar_hq',
    creator: '@goosar_hq',
  },
  alternates: {
    canonical: '/',
  },
  robots: {
    index: true,
    follow: true,
  },
};

const HTML_LANG: Record<SupportedLocale, string> = {
  en: 'en',
  'zh-Hans': 'zh-CN',
  ko: 'ko-KR',
  ja: 'ja-JP',
  ru: 'ru-RU',
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const locale = await getRequestLocale();
  const resources = { [locale]: RESOURCES[locale] };
  const apiBaseUrl = resolveBrowserApiBaseUrl(process.env);
  const wsUrl = resolveBrowserWsUrl(process.env);

  return (
    <html
      lang={HTML_LANG[locale]}
      suppressHydrationWarning
      className={cn(
        'antialiased font-sans h-full',
        manrope.variable,
        unbounded.variable,
        jetbrainsMono.variable,
      )}
    >
      <body className="h-full overflow-hidden">
        {/*
          react-grab: dev-only element inspector. Hold ⌘C (Mac) / Ctrl+C and click
          any element to copy its source path + line + component stack for pasting
          to an AI. Opt-in per developer: only loads when VITE_REACT_GRAB is set in
          a local, gitignored apps/web/.env.local — it never activates for anyone
          else. Both guards are read server-side, so the <Script> is omitted from
          the HTML entirely unless you opted in. The VITE_ prefix is shared with the
          desktop renderer (apps/desktop/src/renderer/src/main.tsx), where Vite only
          exposes VITE_-prefixed vars to client code, so one var name covers both
          apps. See https://www.react-grab.com/
        */}
        {process.env.NODE_ENV === 'development' && process.env.VITE_REACT_GRAB && (
          <Script
            src="//unpkg.com/react-grab/dist/index.global.js"
            crossOrigin="anonymous"
            strategy="beforeInteractive"
          />
        )}
        <ThemeProvider>
          <WebProviders locale={locale} resources={resources} apiBaseUrl={apiBaseUrl} wsUrl={wsUrl}>
            {children}
          </WebProviders>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
