'use client';

import { useCallback, useMemo } from 'react';
import { useQuery, type QueryKey } from '@tanstack/react-query';
import type { Issue, IssueAssigneeGroup, Project } from '@goosar/core/types';
import { ALL_STATUSES } from '@goosar/core/issues/config';
import { projectListOptions } from '@goosar/core/projects/queries';
import {
  childIssueProgressOptions,
  type AssigneeGroupedIssuesFilter,
  type IssueSortParam,
  type MyIssuesFilter,
} from '@goosar/core/issues/queries';
import {
  issueSurfaceAssigneeGroupsOptions,
  issueSurfaceGanttOptions,
  issueSurfaceListOptions,
} from '@goosar/core/issues/surface/repository';
import {
  useRealtimePollingInterval,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
} from '@goosar/core/realtime';
import type { IssueSurfaceQueryPlan } from '@goosar/core/issues/surface/query-plan';
import type { IssueStatus } from '@goosar/core/types';
import {
  applyIssueFilters,
  filterAssigneeGroups,
  type IssueFilterState,
  type IssueFilters,
} from '../utils/filter';
import type { ChildProgress } from '../components/list-row';
import type { IssueStatusBranches, IssueStatusPagination } from './use-issue-status-branches';
import type { IssueGroupBranches } from './use-issue-group-branches';

const EMPTY_ISSUES: Issue[] = [];
const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();
const EMPTY_PROJECTS: Project[] = [];

function ganttCanvasRows(issues: Issue[], showCompleted: boolean): Issue[] {
  const dated = issues.filter((i) => i.start_date || i.due_date);
  if (showCompleted) return dated;
  return dated.filter((i) => i.status !== 'done' && i.status !== 'cancelled');
}

export interface IssueSurfaceData {
  surfaceIssues: Issue[];
  projectIssues: Issue[];
  issues: Issue[];
  swimlaneIssues: Issue[];
  workingScopeIssues: Issue[] | undefined;
  filteredGanttIssues: Issue[];
  assigneeGroups?: IssueAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedIssuesFilter;
  filter: MyIssuesFilter;
  loadMoreScope?: string;
  loadMoreFilter?: MyIssuesFilter;
  ganttIssues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  statusPagination: IssueStatusPagination;
  activeFilters: Omit<IssueFilters, 'statusFilters'>;
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  resolveTableExportLookups: (needs: { projects: boolean; childProgress: boolean }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
  isLoading: boolean;
  isRefreshing: boolean;
  isEmpty: boolean;
}

export function useIssueSurfaceData({
  wsId,
  queryPlan,
  projectId,
  usesAssigneeBoard,
  usesGantt,
  usesTable,
  serverStatusBranches,
  serverGroupBranches,
  ganttShowCompleted,
  sort,
  statusFilters,
  priorityFilters,
  assigneeFilters,
  includeNoAssignee,
  agentRunningFilter,
  creatorFilters,
  projectFilters,
  includeNoProject,
  labelFilters,
  propertyFilters,
  workingIssueIDs,
  showSubIssues,
  loadProjects,
  projectsAreLaneSet,
}: {
  wsId: string;
  queryPlan: IssueSurfaceQueryPlan;
  projectId?: string;
  usesAssigneeBoard: boolean;
  usesGantt: boolean;
  usesTable: boolean;
  serverStatusBranches: IssueStatusBranches;
  serverGroupBranches: IssueGroupBranches;
  ganttShowCompleted: boolean;
  sort: IssueSortParam;
  statusFilters: IssueStatus[];
  priorityFilters: IssueFilterState['priorityFilters'];
  assigneeFilters: IssueFilterState['assigneeFilters'];
  includeNoAssignee: boolean;
  agentRunningFilter: boolean;
  creatorFilters: IssueFilterState['creatorFilters'];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters: Record<string, string[]>;
  workingIssueIDs: ReadonlySet<string>;
  showSubIssues: boolean;
  loadProjects: boolean;
  projectsAreLaneSet: boolean;
}): IssueSurfaceData {
  const assigneeGroupFilter = useMemo<AssigneeGroupedIssuesFilter>(
    () => ({
      ...queryPlan.groupedScopeFilter,
      statuses: statusFilters.length > 0 ? statusFilters : [...ALL_STATUSES],
      priorities: priorityFilters,
      assignee_filters: assigneeFilters,
      include_no_assignee: includeNoAssignee,
      creator_filters: creatorFilters,
      project_ids: projectFilters,
      include_no_project: includeNoProject,
      label_ids: labelFilters,
    }),
    [
      assigneeFilters,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      queryPlan.groupedScopeFilter,
      statusFilters,
    ],
  );

  const activeAssigneeGroupsOptions = issueSurfaceAssigneeGroupsOptions(
    wsId,
    queryPlan,
    assigneeGroupFilter,
    sort,
  );

  const surfacePollingInterval = useRealtimePollingInterval(BACKGROUND_DEGRADED_POLL_INTERVAL_MS);
  const statusIssuesQuery = useQuery({
    ...issueSurfaceListOptions(wsId, queryPlan, sort),
    enabled:
      !usesAssigneeBoard &&
      !usesGantt &&
      !usesTable &&
      !serverStatusBranches.enabled &&
      !serverGroupBranches.enabled,
    refetchInterval: surfacePollingInterval,
  });
  const assigneeGroupsQuery = useQuery({
    ...activeAssigneeGroupsOptions,
    enabled: usesAssigneeBoard && !serverGroupBranches.enabled,
    refetchInterval: surfacePollingInterval,
  });
  const ganttIssuesQuery = useQuery({
    ...issueSurfaceGanttOptions(wsId, projectId ?? ''),
    enabled: usesGantt,
    refetchInterval: surfacePollingInterval,
  });
  const hasWorkingIssues = workingIssueIDs.size > 0;
  const workingFilterContext = useMemo(
    () => ({ runningIssueIds: workingIssueIDs }),
    [workingIssueIDs],
  );
  const bucketedIssues = useMemo(() => {
    return serverStatusBranches.enabled
      ? serverStatusBranches.issues
      : serverGroupBranches.enabled
        ? serverGroupBranches.issues
        : usesAssigneeBoard
          ? (assigneeGroupsQuery.data?.groups.flatMap((group) => group.issues) ?? [])
          : (statusIssuesQuery.data ?? EMPTY_ISSUES);
  }, [
    assigneeGroupsQuery.data?.groups,
    serverStatusBranches.enabled,
    serverStatusBranches.issues,
    serverGroupBranches.enabled,
    serverGroupBranches.issues,
    statusIssuesQuery.data,
    usesAssigneeBoard,
  ]);

  const ganttIssues = ganttIssuesQuery.data ?? EMPTY_ISSUES;
  const surfaceIssues = usesGantt ? ganttIssues : usesTable ? EMPTY_ISSUES : bucketedIssues;

  const baseFilterState = useMemo<IssueFilterState>(
    () => ({
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      workingOnly: agentRunningFilter,
      showSubIssues,
    }),
    [
      assigneeFilters,
      agentRunningFilter,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      propertyFilters,
      showSubIssues,
      statusFilters,
    ],
  );

  const issues = useMemo(
    () =>
      serverStatusBranches.enabled
        ? surfaceIssues
        : applyIssueFilters(surfaceIssues, baseFilterState, workingFilterContext),
    [baseFilterState, serverStatusBranches.enabled, surfaceIssues, workingFilterContext],
  );

  const statuslessFilterState = useMemo<IssueFilterState>(
    () => ({
      ...baseFilterState,
      statusFilters: [],
    }),
    [baseFilterState],
  );

  const swimlaneIssues = useMemo(
    () => applyIssueFilters(surfaceIssues, statuslessFilterState, workingFilterContext),
    [statuslessFilterState, surfaceIssues, workingFilterContext],
  );

  const filteredGanttIssues = useMemo(
    () =>
      ganttCanvasRows(
        applyIssueFilters(ganttIssues, baseFilterState, workingFilterContext),
        ganttShowCompleted,
      ),
    [baseFilterState, ganttIssues, ganttShowCompleted, workingFilterContext],
  );

  const filteredAssigneeGroups = useMemo(
    () =>
      filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
        agentRunningFilter,
        runningIssueIds: workingIssueIDs,
        showSubIssues,
        propertyFilters,
      }),
    [
      assigneeGroupsQuery.data?.groups,
      agentRunningFilter,
      propertyFilters,
      showSubIssues,
      workingIssueIDs,
    ],
  );

  const workingFilterState = useMemo<IssueFilterState>(
    () => ({
      ...baseFilterState,
      workingOnly: true,
    }),
    [baseFilterState],
  );

  const workingScopeIssues = useMemo(() => {
    if (usesGantt) {
      return ganttCanvasRows(
        applyIssueFilters(ganttIssues, workingFilterState, workingFilterContext),
        ganttShowCompleted,
      );
    }
    if (usesAssigneeBoard && !serverGroupBranches.enabled) {
      const groupedIssues = (
        filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
          agentRunningFilter: true,
          runningIssueIds: workingIssueIDs,
          showSubIssues,
          propertyFilters,
        }) ?? []
      ).flatMap((group) => group.issues);
      return applyIssueFilters(groupedIssues, workingFilterState, workingFilterContext);
    }
    if (usesTable || serverStatusBranches.enabled || serverGroupBranches.enabled) {
      if (!hasWorkingIssues) return EMPTY_ISSUES;
      return undefined;
    }
    return applyIssueFilters(surfaceIssues, workingFilterState, workingFilterContext);
  }, [
    assigneeGroupsQuery.data?.groups,
    ganttIssues,
    ganttShowCompleted,
    hasWorkingIssues,
    propertyFilters,
    showSubIssues,
    surfaceIssues,
    usesAssigneeBoard,
    usesGantt,
    usesTable,
    serverStatusBranches.enabled,
    serverGroupBranches.enabled,
    workingFilterState,
    workingFilterContext,
    workingIssueIDs,
  ]);

  const { data: childProgressData, refetch: refetchChildProgress } = useQuery({
    ...childIssueProgressOptions(wsId),
    refetchInterval: surfacePollingInterval,
  });
  const childProgressMap = childProgressData ?? EMPTY_CHILD_PROGRESS;
  const { data: projectData, refetch: refetchProjects } = useQuery({
    ...projectListOptions(wsId),
    enabled: loadProjects,
    refetchInterval: projectsAreLaneSet ? surfacePollingInterval : false,
  });
  const projects = projectData ?? EMPTY_PROJECTS;
  const projectMap = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );
  const resolveTableExportLookups = useCallback(
    async (needs: { projects: boolean; childProgress: boolean }) => {
      const [projectResult, progressResult] = await Promise.all([
        needs.projects ? refetchProjects() : Promise.resolve(null),
        needs.childProgress ? refetchChildProgress() : Promise.resolve(null),
      ]);
      if (projectResult?.error) throw projectResult.error;
      if (progressResult?.error) throw progressResult.error;
      if (needs.projects && !projectResult?.data) {
        throw new Error('Failed to load project data for export');
      }
      if (needs.childProgress && !progressResult?.data) {
        throw new Error('Failed to load child progress for export');
      }
      const resolvedProjects = projectResult?.data ?? projects;
      return {
        projectMap: new Map(resolvedProjects.map((project) => [project.id, project])),
        childProgressMap: progressResult?.data ?? childProgressMap,
      };
    },
    [childProgressMap, projects, refetchChildProgress, refetchProjects],
  );

  const visibleStatuses = useMemo<IssueStatus[]>(() => {
    if (statusFilters.length > 0) {
      return ALL_STATUSES.filter((s) => statusFilters.includes(s));
    }
    return ALL_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo<IssueStatus[]>(
    () => ALL_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const activeFilters = useMemo(
    () => ({
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      agentRunningFilter,
      runningIssueIds: workingIssueIDs,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      showSubIssues,
    }),
    [
      assigneeFilters,
      agentRunningFilter,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      propertyFilters,
      priorityFilters,
      projectFilters,
      showSubIssues,
      workingIssueIDs,
    ],
  );

  const isLoading = serverGroupBranches.enabled
    ? serverGroupBranches.isLoading
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isLoading
      : usesGantt
        ? ganttIssuesQuery.isLoading
        : usesTable
          ? false
          : serverStatusBranches.enabled
            ? serverStatusBranches.isLoading
            : statusIssuesQuery.isLoading;

  const isRefreshing = serverGroupBranches.enabled
    ? serverGroupBranches.isRefreshing
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isPlaceholderData
      : usesGantt
        ? false
        : usesTable
          ? false
          : serverStatusBranches.enabled
            ? serverStatusBranches.isRefreshing
            : statusIssuesQuery.isPlaceholderData;

  return {
    surfaceIssues,
    projectIssues: surfaceIssues,
    issues,
    swimlaneIssues,
    workingScopeIssues,
    filteredGanttIssues,
    assigneeGroups: usesAssigneeBoard ? filteredAssigneeGroups : undefined,
    assigneeGroupQueryKey: usesAssigneeBoard ? activeAssigneeGroupsOptions.queryKey : undefined,
    assigneeGroupFilter: usesAssigneeBoard ? assigneeGroupFilter : undefined,
    filter: queryPlan.queryFilter,
    loadMoreScope: queryPlan.loadMoreScope,
    loadMoreFilter: queryPlan.loadMoreFilter,
    ganttIssues,
    visibleStatuses,
    hiddenStatuses,
    statusPagination: serverStatusBranches.pagination,
    activeFilters,
    childProgressMap,
    projectMap,
    resolveTableExportLookups,
    isLoading,
    isRefreshing,
    isEmpty:
      !isLoading &&
      !usesGantt &&
      !usesTable &&
      (serverStatusBranches.enabled
        ? serverStatusBranches.isTotalKnown && serverStatusBranches.total === 0
        : serverGroupBranches.enabled
          ? !serverGroupBranches.isError && serverGroupBranches.total === 0
          : surfaceIssues.length === 0),
  };
}
