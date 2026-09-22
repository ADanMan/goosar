'use client';

import { CalendarDays } from 'lucide-react';
import type { UpdateProjectRequest } from '@goosar/core/types';
import { DateOnlyPicker } from '../../common/date-only-picker';
import { useT } from '../../i18n';

export function ProjectDueDatePicker({
  dueDate,
  onUpdate,
  triggerRender,
  align = 'start',
  open,
  onOpenChange,
}: {
  dueDate: string | null;
  onUpdate: (updates: Partial<UpdateProjectRequest>) => void;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  align?: 'start' | 'center' | 'end';
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
}) {
  const { t } = useT('projects');
  return (
    <DateOnlyPicker
      value={dueDate}
      onChange={(v) => onUpdate({ due_date: v })}
      icon={<CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.detail.prop_due_date)}
      clearLabel={t(($) => $.detail.clear_date)}
      highlightOverdue
      triggerRender={triggerRender}
      align={align}
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}
