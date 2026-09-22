import { Monitor } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';

const BADGE_PALETTE = [
  'fill-brand',
  'fill-success',
  'fill-warning',
  'fill-info',
  'fill-destructive',
  'fill-chart-3',
] as const;

function runtimeLetter(provider: string): string | null {
  const match = /^runtime-([a-z])$/i.exec(provider.trim());
  return match ? (match[1] ?? '').toUpperCase() : null;
}

export function ProviderLogo({
  provider,
  className = 'h-4 w-4',
}: {
  provider: string;
  className?: string;
}) {
  const letter = runtimeLetter(provider);
  if (!letter) return <Monitor className={className} />;

  const fill = BADGE_PALETTE[letter.codePointAt(0)! % BADGE_PALETTE.length]!;

  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <rect width="24" height="24" rx="6" className={cn(fill, 'opacity-15')} />
      <text
        x="12"
        y="12"
        textAnchor="middle"
        dominantBaseline="central"
        className={cn(fill, 'font-semibold')}
        style={{ fontSize: 13 }}
      >
        {letter}
      </text>
    </svg>
  );
}
