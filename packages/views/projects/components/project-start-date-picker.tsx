'use client';

import { CalendarClock } from 'lucide-react';
import type { UpdateProjectRequest } from '@goosar/core/types';
import { DateOnlyPicker } from '../../common/date-only-picker';
import { useT } from '../../i18n';

export function ProjectStartDatePicker({
  startDate,
  onUpdate,
  triggerRender,
  align = 'start',
  open,
  onOpenChange,
}: {
  startDate: string | null;
  onUpdate: (updates: Partial<UpdateProjectRequest>) => void;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  align?: 'start' | 'center' | 'end';
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
}) {
  const { t } = useT('projects');
  return (
    <DateOnlyPicker
      value={startDate}
      onChange={(v) => onUpdate({ start_date: v })}
      icon={<CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.detail.prop_start_date)}
      clearLabel={t(($) => $.detail.clear_date)}
      triggerRender={triggerRender}
      align={align}
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}
