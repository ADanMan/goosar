import type { Metadata } from 'next';
import { GoosarLanding } from '@/features/landing/components/goosar-landing';

export const metadata: Metadata = {
  title: 'Homepage',
  description:
    'Goosar — agentic workplaces for business. Assign a task to an agent the way you assign it to an employee: it does the work and returns a finished result.',
  openGraph: {
    title: 'Goosar — Agentic Workplaces for Business',
    description: 'People and agents in one flow of tasks, under human control.',
    url: '/homepage',
  },
  alternates: {
    canonical: '/homepage',
  },
};

export default function HomepagePage() {
  return <GoosarLanding />;
}
