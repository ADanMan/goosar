import type { IssueStatus } from '@goosar/core/types';
import { StatusIcon } from './status-icon';
import { useT } from '../../i18n';

// Same 5-stage token set as the row/card status stripe (task-properties.tsx);
// blocked/cancelled fall back to the nearest semantic token.
const STATUS_DOT_CLASS: Record<IssueStatus, string> = {
  backlog: 'bg-status-backlog',
  todo: 'bg-status-todo',
  in_progress: 'bg-status-doing',
  in_review: 'bg-status-review',
  done: 'bg-status-done',
  blocked: 'bg-destructive',
  cancelled: 'bg-muted-foreground',
};

export function StatusHeading({ status, count }: { status: IssueStatus; count: number }) {
  const { t } = useT('issues');
  return (
    <div className="flex items-center gap-2">
      <span className="inline-flex items-center gap-1.5 text-xs font-semibold">
        <span aria-hidden className={`size-1.5 rounded-full ${STATUS_DOT_CLASS[status] ?? 'bg-muted-foreground'}`} />
        <StatusIcon status={status} className="h-3 w-3" />
        {t(($) => $.status[status])}
      </span>
      <span className="text-xs text-muted-foreground">{count}</span>
    </div>
  );
}
