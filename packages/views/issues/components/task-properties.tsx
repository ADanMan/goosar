import type { IssueStatus } from '@goosar/core/types';

// Status tokens only cover the 5 core stages (bg-status-backlog|todo|doing|review|done);
// blocked/cancelled reuse the nearest semantic token rather than adding new ones.
const STATUS_STRIPE_CLASS: Record<IssueStatus, string> = {
  backlog: 'bg-status-backlog',
  todo: 'bg-status-todo',
  in_progress: 'bg-status-doing',
  in_review: 'bg-status-review',
  done: 'bg-status-done',
  blocked: 'bg-destructive',
  cancelled: 'bg-muted-foreground',
};

/**
 * 3px status-colored stripe, shared between the list row and the board card
 * so both surfaces render status the same way (bg-status-* tokens, no
 * colored badges/backgrounds elsewhere on the item).
 */
export function StatusStripe({
  status,
  className = '',
}: {
  status: IssueStatus | string;
  className?: string;
}) {
  const colorClass = STATUS_STRIPE_CLASS[status as IssueStatus] ?? 'bg-muted-foreground';
  return (
    <span
      aria-hidden
      className={`absolute inset-y-0 left-0 w-[3px] shrink-0 ${colorClass} ${className}`}
    />
  );
}
