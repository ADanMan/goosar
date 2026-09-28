'use client';

import { useEffect } from 'react';
import { X } from 'lucide-react';
import { Alert, AlertDescription } from '@goosar/ui/components/ui/alert';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';

export type SectionNoticeTone = 'success' | 'destructive';

export interface SectionNoticeState {
  tone: SectionNoticeTone;
  message: string;
}

const AUTO_DISMISS_MS = 6000;

/**
 * Inline post-action notification (Carbon "notification pattern"): shown after
 * a section's mutation settles, auto-dismisses after 6s or on explicit close.
 */
export function SectionNotice({
  notice,
  onDismiss,
  dismissLabel,
  className,
}: {
  notice: SectionNoticeState | null;
  onDismiss: () => void;
  dismissLabel: string;
  className?: string;
}) {
  useEffect(() => {
    if (notice === null) return;
    const timer = window.setTimeout(onDismiss, AUTO_DISMISS_MS);
    return () => window.clearTimeout(timer);
  }, [notice, onDismiss]);

  if (notice === null) return null;

  return (
    <Alert
      variant={notice.tone === 'destructive' ? 'destructive' : 'default'}
      className={cn('relative pr-9', className)}
    >
      <AlertDescription>{notice.message}</AlertDescription>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        className="absolute top-1.5 right-1.5"
        onClick={onDismiss}
        aria-label={dismissLabel}
      >
        <X />
      </Button>
    </Alert>
  );
}
