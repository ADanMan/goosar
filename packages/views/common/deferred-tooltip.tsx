'use client';

import {
  cloneElement,
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react';
import { Tooltip, TooltipContent } from '@goosar/ui/components/ui/tooltip';

const TOOLTIP_OPEN_DELAY = 200;

export function DeferredTooltip({
  trigger,
  content,
  side,
}: {
  trigger: ReactElement<Record<string, unknown>>;
  content: ReactNode;
  side?: 'top' | 'bottom' | 'left' | 'right';
}) {
  const anchorRef = useRef<HTMLElement | null>(null);
  const [warm, setWarm] = useState(false);
  const [open, setOpen] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTimer = () => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };
  useEffect(() => clearTimer, []);

  const element = cloneElement(trigger, {
    ref: (el: HTMLElement | null) => {
      anchorRef.current = el;
    },
    onPointerEnter: (e: React.PointerEvent) => {
      (trigger.props.onPointerEnter as ((e: React.PointerEvent) => void) | undefined)?.(e);
      setWarm(true);
      clearTimer();
      timerRef.current = setTimeout(() => setOpen(true), TOOLTIP_OPEN_DELAY);
    },
    onPointerLeave: (e: React.PointerEvent) => {
      (trigger.props.onPointerLeave as ((e: React.PointerEvent) => void) | undefined)?.(e);
      clearTimer();
      setOpen(false);
    },
  });

  return (
    <>
      {element}
      {warm && (
        <Tooltip open={open} onOpenChange={setOpen}>
          <TooltipContent side={side} anchor={anchorRef}>
            {content}
          </TooltipContent>
        </Tooltip>
      )}
    </>
  );
}
