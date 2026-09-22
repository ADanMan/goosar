import type { Issue, IssueStatus, IssuePriority, IssueAssigneeGroup } from '@goosar/core/types';
import type { ActorFilterValue } from '@goosar/core/issues/stores/view-store';
import type { IssueActivityState } from '../surface/activity';

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters?: Record<string, string[]>;
  agentRunningFilter?: boolean;
  runningIssueIds?: ReadonlySet<string>;
  showSubIssues?: boolean;
}

export interface IssueFilterState {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters?: Record<string, string[]>;
  workingOnly: boolean;
  showSubIssues?: boolean;
}

export interface IssueFilterContext {
  activityByIssueId?: ReadonlyMap<string, IssueActivityState>;
  runningIssueIds?: ReadonlySet<string>;
}

export function issueMatchesPropertyFilters(
  issue: Issue,
  propertyFilters: Record<string, string[]> | undefined,
): boolean {
  if (!propertyFilters) return true;
  for (const [propertyId, selected] of Object.entries(propertyFilters)) {
    if (selected.length === 0) continue;
    const value = issue.properties?.[propertyId];
    if (value === undefined) return false;
    if (typeof value === 'string') {
      if (!selected.includes(value)) return false;
    } else if (Array.isArray(value)) {
      if (!value.some((id) => selected.includes(id))) return false;
    } else if (typeof value === 'boolean') {
      if (!selected.includes(String(value))) return false;
    } else {
      return false;
    }
  }
  return true;
}

function issueIsWorking(issueId: string, context: IssueFilterContext) {
  if (context.activityByIssueId) {
    return context.activityByIssueId.get(issueId)?.isWorking === true;
  }
  return context.runningIssueIds?.has(issueId) === true;
}

export function applyIssueFilters(
  issues: Issue[],
  filters: IssueFilterState,
  context: IssueFilterContext = {},
): Issue[] {
  const {
    statusFilters,
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    creatorFilters,
    projectFilters,
    includeNoProject,
    labelFilters,
    workingOnly,
  } = filters;
  const hasAssigneeFilter =
    filters.assigneeFilterActive === true || assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  const applyWorkingOnly = workingOnly === true;
  const hideSubIssues = filters.showSubIssues === false;

  return issues.filter((issue) => {
    if (applyWorkingOnly && !issueIsWorking(issue.id, context)) return false;

    if (hideSubIssues && issue.parent_issue_id) return false;

    if (statusFilters.length > 0 && !statusFilters.includes(issue.status)) return false;

    if (priorityFilters.length > 0 && !priorityFilters.includes(issue.priority)) return false;

    if (hasAssigneeFilter) {
      if (!issue.assignee_id) {
        if (!includeNoAssignee) return false;
      } else if (assigneeFilters.length > 0) {
        if (
          !assigneeFilters.some((f) => f.type === issue.assignee_type && f.id === issue.assignee_id)
        )
          return false;
      } else {
        return false;
      }
    }

    if (
      creatorFilters.length > 0 &&
      !creatorFilters.some((f) => f.type === issue.creator_type && f.id === issue.creator_id)
    ) {
      return false;
    }

    if (hasProjectFilter) {
      if (!issue.project_id) {
        if (!includeNoProject) return false;
      } else if (projectFilters.length > 0) {
        if (!projectFilters.includes(issue.project_id)) return false;
      } else {
        return false;
      }
    }

    if (labelFilters.length > 0) {
      const issueLabels = issue.labels;
      if (!issueLabels || issueLabels.length === 0) return false;
      if (!issueLabels.some((l) => labelFilters.includes(l.id))) return false;
    }

    if (!issueMatchesPropertyFilters(issue, filters.propertyFilters)) return false;

    return true;
  });
}

export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  return applyIssueFilters(
    issues,
    {
      statusFilters: filters.statusFilters,
      priorityFilters: filters.priorityFilters,
      assigneeFilters: filters.assigneeFilters,
      includeNoAssignee: filters.includeNoAssignee,
      assigneeFilterActive: filters.assigneeFilterActive,
      creatorFilters: filters.creatorFilters,
      projectFilters: filters.projectFilters,
      includeNoProject: filters.includeNoProject,
      labelFilters: filters.labelFilters,
      propertyFilters: filters.propertyFilters,
      workingOnly: filters.agentRunningFilter === true,
      showSubIssues: filters.showSubIssues,
    },
    { runningIssueIds: filters.runningIssueIds },
  );
}

export function filterAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  filters: {
    showSubIssues?: boolean;
    agentRunningFilter?: boolean;
    runningIssueIds?: ReadonlySet<string>;
    propertyFilters?: Record<string, string[]>;
  },
): IssueAssigneeGroup[] | undefined {
  const applyRunning = filters.agentRunningFilter === true;
  const hideSubIssues = filters.showSubIssues === false;
  const hasPropertyFilters = Object.values(filters.propertyFilters ?? {}).some(
    (selected) => selected.length > 0,
  );
  if (!groups || (!applyRunning && !hideSubIssues && !hasPropertyFilters)) return groups;

  const { runningIssueIds } = filters;
  return groups
    .map((group) => {
      const issues = group.issues.filter((issue) => {
        if (applyRunning && !(runningIssueIds?.has(issue.id) ?? false)) return false;
        if (hideSubIssues && issue.parent_issue_id) return false;
        if (hasPropertyFilters && !issueMatchesPropertyFilters(issue, filters.propertyFilters))
          return false;
        return true;
      });
      return { ...group, issues, total: issues.length };
    })
    .filter((group) => group.issues.length > 0);
}
