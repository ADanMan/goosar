import { cn } from '@goosar/ui/lib/utils';

export const githubUrl = 'https://github.com/adanman/goosar';

export function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true" className={className} fill="currentColor">
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2 .37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.65 7.65 0 0 1 8 4.84c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

export function ImageIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="3.5" y="5" width="17" height="14" rx="2.5" />
      <circle cx="9" cy="10" r="1.6" />
      <path d="m20.5 16-4.8-4.8a1 1 0 0 0-1.4 0L8 17.5" />
      <path d="m11.5 14.5 1.8-1.8a1 1 0 0 1 1.4 0l2.8 2.8" />
    </svg>
  );
}

export type LandingVariant = 'dark' | 'light' | 'hero';

// Both `dark` and `hero` float the header over an always-dark band (the
// download hero and the homepage hero share the same --inverse/--rail
// surface in both themes), so they share one inverse treatment; `light`
// is the plain-page header (e.g. /about).
function isInverseVariant(variant: LandingVariant) {
  return variant !== 'light';
}

// One primary-button shape for every landing CTA: header, hero, and the
// section CTAs that route through it. Same height/padding/radius/type for
// solid and ghost so buttons of equal importance always read the same.
const BUTTON_BASE =
  'inline-flex h-10 items-center justify-center gap-2 rounded-md px-4 text-sm font-medium transition-colors';

export function headerButtonClassName(tone: 'ghost' | 'solid', variant: LandingVariant = 'dark') {
  if (tone === 'solid') {
    return cn(BUTTON_BASE, 'bg-brand text-brand-foreground hover:bg-brand/90');
  }
  return cn(
    BUTTON_BASE,
    isInverseVariant(variant)
      ? 'text-inverse-foreground hover:bg-inverse-foreground/10'
      : 'text-foreground hover:bg-muted',
  );
}

export function heroButtonClassName(tone: 'ghost' | 'solid') {
  if (tone === 'solid') {
    return cn(BUTTON_BASE, 'bg-brand text-brand-foreground hover:bg-brand/90');
  }
  return cn(
    BUTTON_BASE,
    'border border-inverse-foreground/20 text-inverse-foreground backdrop-blur-sm hover:bg-inverse-foreground/10',
  );
}
