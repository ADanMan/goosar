import type { CSSProperties } from 'react';

export function DragStrip() {
  return (
    <div
      aria-hidden
      className="h-12 shrink-0"
      style={{ WebkitAppRegion: 'drag' } as CSSProperties}
    />
  );
}
