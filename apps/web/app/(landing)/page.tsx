import type { Metadata } from 'next';
import { GoosarLanding } from '@/features/landing/components/goosar-landing';
import { RedirectIfAuthenticated } from '@/features/landing/components/redirect-if-authenticated';

export const metadata: Metadata = {
  title: {
    absolute: 'Goosar — Agentic Workplaces for Business',
  },
  description:
    'Assign a task to an agent the way you assign it to an employee: it does the work and returns a finished result. People and agents in one flow of tasks, under human control.',
  openGraph: {
    title: 'Goosar — Agentic Workplaces for Business',
    description: 'People and agents in one flow of tasks, under human control.',
    url: '/',
  },
  alternates: {
    canonical: '/',
  },
};

export default function LandingPage() {
  return (
    <>
      <RedirectIfAuthenticated />
      <GoosarLanding />
    </>
  );
}
