'use client';

import { createContext, use } from 'react';

const GithubStarsContext = createContext<number | null>(null);

export function GithubStarsProvider({
  stars,
  children,
}: {
  stars: number | null;
  children: React.ReactNode;
}) {
  return <GithubStarsContext.Provider value={stars}>{children}</GithubStarsContext.Provider>;
}

export function useGithubStars(): number | null {
  return use(GithubStarsContext);
}

export function formatStarCount(n: number): string {
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}m`;
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}k`;
  }
  return String(n);
}
