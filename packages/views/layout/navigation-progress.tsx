'use client';

import { useEffect, useState } from 'react';

import { useIsNavigating } from '../navigation';

export function NavigationProgress() {
  const isNavigating = useIsNavigating();
  const [renderSweep, setRenderSweep] = useState(false);

  useEffect(() => {
    if (isNavigating) setRenderSweep(true);
  }, [isNavigating]);

  return (
    <div
      aria-hidden
      data-visible={isNavigating ? 'true' : 'false'}
      onTransitionEnd={(event) => {
        if (event.propertyName === 'opacity' && !isNavigating) {
          setRenderSweep(false);
        }
      }}
      className="pointer-events-none absolute inset-x-0 top-0 z-50 h-0.5 overflow-hidden opacity-0 transition-opacity duration-200 data-[visible=true]:opacity-100"
    >
      {renderSweep && (
        <div
          className="h-full w-1/3 animate-nav-progress-sweep bg-brand"
          style={{
            boxShadow:
              '0 0 8px color-mix(in oklab, var(--brand) 60%, transparent), 0 0 2px color-mix(in oklab, var(--brand) 80%, transparent)',
          }}
        />
      )}
    </div>
  );
}
