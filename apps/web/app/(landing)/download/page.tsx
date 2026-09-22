import type { Metadata } from 'next';
import { fetchLatestRelease } from '@/features/landing/utils/github-release';
import { readConfiguredAssets } from '@/features/landing/utils/configured-assets';
import { DownloadClient } from './download-client';

export const dynamic = 'force-dynamic';

export const metadata: Metadata = {
  title: 'Download Goosar',
  description:
    'Download Goosar for macOS, Windows, or Linux — or install the CLI for servers and remote dev boxes.',
  openGraph: {
    title: 'Download Goosar',
    description:
      'Get the Goosar desktop app with a bundled daemon, or install the CLI for servers and remote dev boxes.',
    url: '/download',
  },
  alternates: {
    canonical: '/download',
  },
};

export default async function DownloadPage() {
  const release = await fetchLatestRelease();
  const configuredAssets = readConfiguredAssets(process.env);
  return <DownloadClient release={release} configuredAssets={configuredAssets} />;
}
