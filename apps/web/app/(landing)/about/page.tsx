import type { Metadata } from 'next';
import { AboutPageClient } from '@/features/landing/components/about-page-client';

export const metadata: Metadata = {
  title: 'About',
  description:
    'Learn about Goosar — an open-source project management platform where humans and coding agents work side by side.',
  openGraph: {
    title: 'About Goosar',
    description:
      "The story behind Goosar and why we're building project management for human + agent teams.",
    url: '/about',
  },
  alternates: {
    canonical: '/about',
  },
};

export default function AboutPage() {
  return <AboutPageClient />;
}
