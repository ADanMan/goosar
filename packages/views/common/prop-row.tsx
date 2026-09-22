import type { ReactNode } from 'react';

export function PropRow({
  label,
  children,
  interactive = true,
}: {
  label: ReactNode;
  children: ReactNode;
  interactive?: boolean;
}) {
  return (
    <div
      className={`-mx-2 col-span-2 grid min-h-8 grid-cols-subgrid items-center rounded-md px-2 ${
        interactive ? 'transition-colors hover:bg-accent/50' : ''
      }`}
    >
      <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        {label}
      </span>
      <div className="flex min-w-0 items-center gap-1.5 truncate text-xs">{children}</div>
    </div>
  );
}
