// Конфиг статусов и приоритетов проектов: зеркало packages/core/projects/config.ts
// с мобильной палитрой Tailwind.
import type { ProjectPriority, ProjectStatus } from '@goosar/core/types';

export const PROJECT_STATUSES: ProjectStatus[] = [
  'planned',
  'in_progress',
  'paused',
  'completed',
  'cancelled',
];

export const PROJECT_PRIORITIES: ProjectPriority[] = ['urgent', 'high', 'medium', 'low', 'none'];

export const PROJECT_STATUS_LABEL: Record<ProjectStatus, string> = {
  planned: 'Planned',
  in_progress: 'In Progress',
  paused: 'Paused',
  completed: 'Completed',
  cancelled: 'Cancelled',
};

export const PROJECT_PRIORITY_LABEL: Record<ProjectPriority, string> = {
  urgent: 'Urgent',
  high: 'High',
  medium: 'Medium',
  low: 'Low',
  none: 'No priority',
};

export const PROJECT_STATUS_COLOR: Record<ProjectStatus, string> = {
  planned: '#71717a',
  in_progress: '#f59e0b',
  paused: '#71717a',
  completed: '#3b82f6',
  cancelled: '#a1a1aa',
};

export const PROJECT_PRIORITY_BARS: Record<ProjectPriority, number> = {
  urgent: 4,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};

export function projectStatusLabel(value: string): string {
  return (PROJECT_STATUS_LABEL as Record<string, string>)[value] ?? value;
}

export function projectPriorityLabel(value: string): string {
  return (PROJECT_PRIORITY_LABEL as Record<string, string>)[value] ?? value;
}

export function projectStatusColor(value: string): string {
  return (PROJECT_STATUS_COLOR as Record<string, string>)[value] ?? '#71717a';
}

export function projectPriorityBars(value: string): number {
  return (PROJECT_PRIORITY_BARS as Record<string, number>)[value] ?? 0;
}
