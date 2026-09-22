'use client';

import { cloneElement, useState, type ReactElement, type ReactNode } from 'react';

export function DeferredPopup({
  trigger,
  triggerRender,
  triggerClassName,
  ariaHasPopup = 'dialog',
  children,
}: {
  trigger?: ReactNode;
  triggerRender?: ReactElement<Record<string, unknown>>;
  triggerClassName?: string;
  ariaHasPopup?: 'dialog' | 'menu';
  children: (open: boolean, onOpenChange: (v: boolean) => void) => ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);

  if (mounted) {
    return <>{children(open, setOpen)}</>;
  }

  const mountOpen = () => {
    setMounted(true);
    setOpen(true);
  };
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      e.stopPropagation();
      mountOpen();
    }
  };
  const handlers = {
    onClick: (e: React.MouseEvent) => {
      e.stopPropagation();
      mountOpen();
    },
    onKeyDown: handleKeyDown,
    'aria-haspopup': ariaHasPopup,
  };

  if (triggerRender) {
    if (triggerRender.props.children != null) {
      return cloneElement(triggerRender, handlers);
    }
    return cloneElement(triggerRender, handlers, trigger);
  }

  return (
    <button type="button" className={triggerClassName} {...handlers}>
      {trigger}
    </button>
  );
}
