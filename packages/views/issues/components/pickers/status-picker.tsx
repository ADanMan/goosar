'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import type { IssueStatus, UpdateIssueRequest } from '@goosar/core/types';
import { ALL_STATUSES, STATUS_CONFIG } from '@goosar/core/issues/config';
import { StatusIcon } from '../status-icon';
import { PropertyPicker, PickerItem } from './property-picker';
import { useT } from '../../../i18n';

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  status: IssueStatus | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: 'start' | 'center' | 'end';
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const { t } = useT('issues');

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (status != null ? (
          <>
            <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{t(($) => $.status[status])}</span>
          </>
        ) : null)
      }
    >
      {ALL_STATUSES.map((s) => {
        const c = STATUS_CONFIG[s];
        return (
          <PickerItem
            key={s}
            selected={s === status}
            hoverClassName={c.hoverBg}
            onClick={() => {
              const previous = status;
              onUpdate({ status: s });
              setOpen(false);
              // ponytail: undo re-applies the previous status via the same
              // onUpdate path; no separate history/queue, good enough for a
              // single-step revert (H3 — user control and freedom).
              if (previous != null && previous !== s) {
                toast.success(t(($) => $.detail.status_changed_toast, { status: t(($) => $.status[s]) }), {
                  action: {
                    label: t(($) => $.detail.undo),
                    onClick: () => onUpdate({ status: previous }),
                  },
                });
              }
            }}
          >
            <StatusIcon status={s} className="h-3.5 w-3.5" />
            <span>{t(($) => $.status[s])}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
