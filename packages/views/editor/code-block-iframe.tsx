'use client';

import { cn } from '@goosar/ui/lib/utils';

interface CodeBlockIframeProps {
  html: string;
  title: string;
  className?: string;
  heightClassName?: string;
}

export function CodeBlockIframe({
  html,
  title,
  className,
  heightClassName = 'h-[480px]',
}: CodeBlockIframeProps) {
  return (
    <iframe
      srcDoc={html}
      sandbox="allow-scripts"
      title={title}
      className={cn(
        'w-full rounded-md border border-border bg-background',
        heightClassName,
        className,
      )}
    />
  );
}
