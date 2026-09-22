/**
 * Вторая строка строки инбокса, зависящая от типа InboxItemType.
 * Должна совпадать с тем, что видит пользователь на вебе/десктопе —
 * это вопрос поведенческого паритета, а не только вёрстки.
 */
import { View } from 'react-native';
import type { InboxItem, InboxItemType, IssueStatus, IssuePriority } from '@goosar/core/types';
import { formatDateOnly } from '@goosar/core/issues/date';
import { Text } from '@/components/ui/text';
import { StatusIcon } from '@/components/ui/status-icon';
import { PriorityIcon } from '@/components/ui/priority-icon';
import { useActorLookup } from '@/data/use-actor-name';
import { cn } from '@/lib/utils';

const STATUS_LABEL: Record<IssueStatus, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  done: 'Done',
  blocked: 'Blocked',
  cancelled: 'Cancelled',
};

const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: 'Urgent',
  high: 'High',
  medium: 'Medium',
  low: 'Low',
  none: 'No priority',
};

const TYPE_LABEL: Record<InboxItemType, string> = {
  issue_assigned: 'Assigned',
  issue_subscribed: 'Subscribed',
  unassigned: 'Unassigned',
  assignee_changed: 'Reassigned',
  status_changed: 'Status changed',
  priority_changed: 'Priority changed',
  start_date_changed: 'Start date changed',
  due_date_changed: 'Due date changed',
  new_comment: 'New comment',
  mentioned: 'Mentioned',
  review_requested: 'Review requested',
  task_completed: 'Task completed',
  task_failed: 'Task failed',
  agent_blocked: 'Agent blocked',
  agent_completed: 'Agent completed',
  reaction_added: 'Reaction added',
  quick_create_done: 'Quick-create done',
  quick_create_failed: 'Quick-create failed',
  quick_create_unconfirmed: 'Quick-create needs a check',
};

function shortDate(dateStr: string): string {
  return formatDateOnly(dateStr, { month: 'short', day: 'numeric' }, 'en-US');
}

function singleLine(value: string | null | undefined): string {
  return (value ?? '').replace(/\s+/g, ' ').trim();
}

export function InboxDetailLabel({ item, className }: { item: InboxItem; className?: string }) {
  const { getName } = useActorLookup();
  const details = item.details ?? {};

  if (item.type === 'status_changed' && details.to) {
    const status = details.to as IssueStatus;
    return (
      <View className={cn('flex-row items-center gap-1', className)}>
        <Text className="text-xs text-muted-foreground">Set status to</Text>
        <StatusIcon status={status} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {STATUS_LABEL[status] ?? status}
        </Text>
      </View>
    );
  }

  if (item.type === 'priority_changed' && details.to) {
    const priority = details.to as IssuePriority;
    return (
      <View className={cn('flex-row items-center gap-1', className)}>
        <Text className="text-xs text-muted-foreground">Set priority to</Text>
        <PriorityIcon priority={priority} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {PRIORITY_LABEL[priority] ?? priority}
        </Text>
      </View>
    );
  }

  const text = (() => {
    switch (item.type) {
      case 'issue_assigned':
      case 'assignee_changed':
        if (details.new_assignee_id) {
          const name = getName(
            (details.new_assignee_type ?? 'member') as 'member' | 'agent',
            details.new_assignee_id,
          );
          return `Assigned to ${name}`;
        }
        return TYPE_LABEL[item.type];
      case 'unassigned':
        return 'Removed assignee';
      case 'due_date_changed':
        return details.to ? `Set due date to ${shortDate(details.to)}` : 'Removed due date';
      case 'new_comment':
        return singleLine(item.body) || TYPE_LABEL[item.type];
      case 'reaction_added':
        return details.emoji ? `Reacted with ${details.emoji}` : TYPE_LABEL[item.type];
      case 'quick_create_done':
        return details.identifier
          ? `Created with agent: ${details.identifier}`
          : TYPE_LABEL[item.type];
      case 'quick_create_failed': {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail ? `Failed: ${detail}` : TYPE_LABEL[item.type];
      }
      case 'quick_create_unconfirmed': {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail || TYPE_LABEL[item.type];
      }
      default:
        return TYPE_LABEL[item.type] ?? item.type;
    }
  })();

  return (
    <Text className={cn('text-xs text-muted-foreground', className)} numberOfLines={1}>
      {text}
    </Text>
  );
}
