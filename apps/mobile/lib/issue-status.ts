/**
 * Порядок статусов доски и их подписи — копия соответствующего списка из
 * core. Продублировано, а не импортировано, чтобы не тянуть в мобильный
 * клиент веб-специфичные цветовые токены из исходного модуля.
 */
import type { IssuePriority, IssueStatus } from '@goosar/core/types';

export const BOARD_STATUSES: IssueStatus[] = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'done',
  'blocked',
];

export const STATUS_LABEL: Record<IssueStatus, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  done: 'Done',
  blocked: 'Blocked',
  cancelled: 'Cancelled',
};

export const PRIORITY_LABEL: Record<IssuePriority, string> = {
  none: 'No priority',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  urgent: 'Urgent',
};
