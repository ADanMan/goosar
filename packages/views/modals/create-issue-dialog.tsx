'use client';

import { useState } from 'react';
import { cn } from '@goosar/ui/lib/utils';
import { Dialog, DialogContent } from '@goosar/ui/components/ui/dialog';
import { useCreateModeStore, type CreateMode } from '@goosar/core/issues/stores/create-mode-store';
import { AgentCreatePanel } from './quick-create-issue';
import { ManualCreatePanel, manualDialogContentClass } from './create-issue';

export function CreateIssueDialog({
  onClose,
  initialMode,
  data,
}: {
  onClose: () => void;
  initialMode: CreateMode;
  data?: Record<string, unknown> | null;
}) {
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const [mode, setMode] = useState<CreateMode>(initialMode);
  const [panelData, setPanelData] = useState(data ?? null);
  const [isExpanded, setIsExpanded] = useState(false);

  const switchTo = (next: CreateMode) => (carry?: Record<string, unknown> | null) => {
    setLastMode(next);
    setPanelData(carry ?? null);
    setMode(next);
  };

  const className =
    mode === 'agent'
      ? cn(
          'p-0 gap-0 flex flex-col overflow-hidden',
          '!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2',
          '!transition-all !duration-300 !ease-out',
          isExpanded ? '!max-w-4xl !w-full !h-5/6' : '!max-w-xl !w-full !max-h-[80vh]',
        )
      : manualDialogContentClass(isExpanded);

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <DialogContent finalFocus={false} showCloseButton={false} className={className}>
        {mode === 'agent' ? (
          <AgentCreatePanel
            onClose={onClose}
            onSwitchMode={switchTo('manual')}
            data={panelData}
            isExpanded={isExpanded}
            setIsExpanded={setIsExpanded}
          />
        ) : (
          <ManualCreatePanel
            onClose={onClose}
            onSwitchMode={switchTo('agent')}
            data={panelData}
            isExpanded={isExpanded}
            setIsExpanded={setIsExpanded}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
