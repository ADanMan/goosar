import * as React from 'react';
import { ImageOff } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { useBlockedImageLabels } from './blocked-image-label';

export function BlockedImagePlaceholder({
  alt,
  className,
}: {
  alt?: string;
  className?: string;
}): React.JSX.Element {
  const labels = useBlockedImageLabels();
  return (
    <span
      data-blocked-image=""
      role="img"
      aria-label={alt || labels.ariaLabel}
      title={labels.title}
      className={cn(
        'inline-flex max-w-full items-center gap-1.5 rounded-md border border-dashed border-border bg-muted/40 px-2 py-1 align-middle text-xs text-muted-foreground',
        className,
      )}
    >
      <ImageOff aria-hidden="true" className="size-3.5 shrink-0" />
      <span className="truncate">{alt || labels.text}</span>
    </span>
  );
}
