/**
 * Нижний ряд чипов для формы создания задачи. Похож по виду на
 * attribute-row.tsx, но работает с useNewIssueDraftStore вместо объекта
 * issue и мутации. Тап по чипу открывает формSheet-пикер под
 * new-issue-picker/<field>; пикер читает и пишет тот же draft store,
 * поэтому чип обновляется сам при закрытии шита.
 */
import { View } from 'react-native';
import { router } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { AttributeChip } from '@/components/issue/attribute-chip';
import { ActorAvatar } from '@/components/ui/actor-avatar';
import { PriorityIcon } from '@/components/ui/priority-icon';
import { ProjectIcon } from '@/components/ui/project-icon';
import { StatusIcon } from '@/components/ui/status-icon';
import { formatDateOnly } from '@goosar/core/issues/date';
import { useActorLookup } from '@/data/use-actor-name';
import { useNewIssueDraftStore } from '@/data/stores/new-issue-draft-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { PRIORITY_LABEL, STATUS_LABEL } from '@/lib/issue-status';

type NewIssuePickerField = 'status' | 'priority' | 'assignee' | 'project' | 'due-date';

const NEW_ISSUE_PICKER_PATHNAMES = {
  status: '/[workspace]/new-issue-picker/status',
  priority: '/[workspace]/new-issue-picker/priority',
  assignee: '/[workspace]/new-issue-picker/assignee',
  project: '/[workspace]/new-issue-picker/project',
  'due-date': '/[workspace]/new-issue-picker/due-date',
} as const satisfies Record<NewIssuePickerField, string>;

export function CreateFormAttributeRow() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const status = useNewIssueDraftStore((s) => s.status);
  const priority = useNewIssueDraftStore((s) => s.priority);
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const dueDate = useNewIssueDraftStore((s) => s.dueDate);
  const project = useNewIssueDraftStore((s) => s.project);

  const { getName } = useActorLookup();
  const assigneeLabel = assignee ? getName(assignee.type, assignee.id) : 'Assignee';
  const priorityLabel = priority === 'none' ? 'Priority' : PRIORITY_LABEL[priority];

  const open = (field: NewIssuePickerField) => {
    if (!wsSlug) return;
    router.push({
      pathname: NEW_ISSUE_PICKER_PATHNAMES[field],
      params: { workspace: wsSlug },
    });
  };

  return (
    <View>
      <View className="flex-row flex-wrap gap-2">
        <AttributeChip
          icon={<StatusIcon status={status} size={12} />}
          label={STATUS_LABEL[status]}
          variant="filled"
          onPress={() => open('status')}
        />
        <AttributeChip
          icon={<PriorityIcon priority={priority} />}
          label={priorityLabel}
          variant={priority === 'none' ? 'dimmed' : 'filled'}
          onPress={() => open('priority')}
        />
        <AttributeChip
          icon={
            assignee ? (
              <ActorAvatar type={assignee.type} id={assignee.id} size={16} showPresence />
            ) : (
              <Ionicons name="person-circle-outline" size={16} color="#a1a1aa" />
            )
          }
          label={assigneeLabel}
          variant={assignee ? 'filled' : 'dimmed'}
          onPress={() => open('assignee')}
        />
        <AttributeChip
          icon={
            <Ionicons name="calendar-outline" size={14} color={dueDate ? undefined : '#a1a1aa'} />
          }
          label={dueDate ? formatDueDate(dueDate) : 'Due date'}
          variant={dueDate ? 'filled' : 'dimmed'}
          onPress={() => open('due-date')}
        />
        <AttributeChip
          icon={
            project ? (
              <ProjectIcon icon={project.icon} size="sm" />
            ) : (
              <Ionicons name="folder-outline" size={14} color="#a1a1aa" />
            )
          }
          label={project?.title ?? 'Project'}
          variant={project ? 'filled' : 'dimmed'}
          onPress={() => open('project')}
        />
      </View>
    </View>
  );
}

function formatDueDate(iso: string): string {
  return formatDateOnly(iso, { month: 'short', day: 'numeric' }, 'en-US') || 'Due date';
}
