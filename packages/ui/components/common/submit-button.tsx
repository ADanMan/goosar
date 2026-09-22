'use client';

import type { ReactNode } from 'react';
import { ArrowUp, Loader2, Square } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@goosar/ui/components/ui/tooltip';

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
  busy?: boolean;
  running?: boolean;
  onStop?: () => void;
  tooltip?: ReactNode;
  ariaLabel?: string;
  stopTooltip?: ReactNode;
  stopAriaLabel?: string;
}

function SubmitButton({
  onClick,
  disabled,
  loading,
  busy,
  running,
  onStop,
  tooltip,
  ariaLabel,
  stopTooltip,
  stopAriaLabel,
}: SubmitButtonProps) {
  if (running) {
    const stopButton = (
      <Button size="icon-sm" className="rounded-full" onClick={onStop} aria-label={stopAriaLabel}>
        <Square className="fill-current" aria-hidden="true" />
      </Button>
    );
    if (!stopTooltip) return stopButton;
    return (
      <Tooltip>
        <TooltipTrigger render={stopButton} />
        <TooltipContent side="top">{stopTooltip}</TooltipContent>
      </Tooltip>
    );
  }

  const submitButton = (
    <Button
      size="icon-sm"
      className="rounded-full"
      disabled={disabled || loading || busy}
      aria-disabled={busy || undefined}
      aria-busy={busy || undefined}
      onClick={onClick}
      aria-label={ariaLabel}
    >
      {loading || busy ? (
        <Loader2 className="animate-spin" aria-hidden="true" />
      ) : (
        <ArrowUp aria-hidden="true" />
      )}
    </Button>
  );
  if (!tooltip) return submitButton;
  return (
    <Tooltip>
      <TooltipTrigger render={submitButton} />
      <TooltipContent side="top">{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export { SubmitButton, type SubmitButtonProps };
