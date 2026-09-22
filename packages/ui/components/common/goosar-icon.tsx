import { useState, useEffect } from 'react';
import { cn } from '../../lib/utils';
import { GOOSAR_ICON_PATH } from './goosar-icon-path';

interface GoosarIconProps extends React.ComponentProps<'span'> {
  animate?: boolean;
  noSpin?: boolean;
  bordered?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

const borderedSizes = {
  sm: { wrapper: 'p-1.5', icon: 'size-3.5' },
  md: { wrapper: 'p-2', icon: 'size-4' },
  lg: { wrapper: 'p-2.5', icon: 'size-5' },
};

const MARK_VIEW_BOX = '102 100 820 820';

function MarkSvg({ className }: { className?: string }) {
  return (
    <svg
      viewBox={MARK_VIEW_BOX}
      className={cn('block size-full', className)}
      fill="currentColor"
      stroke="none"
      aria-hidden="true"
    >
      <path fillRule="evenodd" d={GOOSAR_ICON_PATH} />
    </svg>
  );
}

export function GoosarIcon({
  className,
  animate = false,
  noSpin = false,
  bordered = false,
  size = 'sm',
  ...props
}: GoosarIconProps) {
  const [entranceDone, setEntranceDone] = useState(!animate);

  useEffect(() => {
    if (!animate) return;
    const timer = setTimeout(() => setEntranceDone(true), 600);
    return () => clearTimeout(timer);
  }, [animate]);

  if (bordered) {
    const sizeConfig = borderedSizes[size];
    return (
      <span
        className={cn(
          'inline-flex items-center justify-center border border-border rounded-md',
          sizeConfig.wrapper,
          className,
        )}
        aria-hidden="true"
        {...props}
      >
        <span
          className={cn(
            'block',
            sizeConfig.icon,
            !entranceDone && 'animate-entrance-spin',
            entranceDone && !noSpin && 'hover:animate-spin',
          )}
        >
          <MarkSvg />
        </span>
      </span>
    );
  }

  return (
    <span
      className={cn(
        'inline-block size-[1em]',
        !entranceDone && 'animate-entrance-spin',
        entranceDone && !noSpin && 'hover:animate-spin',
        className,
      )}
      aria-hidden="true"
      {...props}
    >
      <MarkSvg />
    </span>
  );
}
