'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import type { QueryKey } from '@tanstack/react-query';
import { api } from '@goosar/core/api';
import type {
  Issue,
  IssueAssigneeGroup,
  IssueStatus,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  Project,
} from '@goosar/core/types';
import { workspaceWorkingAgentsOptions } from '@goosar/core/agents';
import { useWorkspaceId } from '@goosar/core/hooks';
import { ALL_STATUSES } from '@goosar/core/issues/config';
import { dateOnlyToLocalDate } from '@goosar/core/issues/date';
import type {
  AssigneeGroupedIssuesFilter,
  IssueSortParam,
  MyIssuesFilter,
} from '@goosar/core/issues/queries';
import { issueTableFacetsOptions } from '@goosar/core/issues/queries';
import {
  buildIssueSurfaceQueryPlan,
  type IssueSurfaceQueryPlan,
} from '@goosar/core/issues/surface/query-plan';
import type { IssueScope } from '@goosar/core/issues/surface/scope';
import type { IssueDateFilter, SortField } from '@goosar/core/issues/stores/view-store';
import { propertyListOptions } from '@goosar/core/properties';
import {
  useRealtimePollingInterval,
  BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
} from '@goosar/core/realtime';
import { propertyIdFromViewKey } from '@goosar/core/issues/stores/view-store';
import { useViewStore } from '@goosar/core/issues/stores/view-store-context';
import type { IssueFilters } from '../utils/filter';
import type { ChildProgress } from '../components/list-row';
import { IssueTableExportIntegrityError } from '../components/table-view-model';
import type { IssueSurfaceMode } from './types';
import type { IssueSurfaceActions } from './actions-context';
import { type IssueSurfaceSelection, useCreateIssueSurfaceSelection } from './selection-context';
import type { IssueCreateDefaults } from './types';
import { useIssueSurfaceActions, type MoveIssueUpdates } from './use-issue-surface-actions';
import { useIssueSurfaceData } from './use-issue-surface-data';
import { useIssueStatusBranches, type IssueStatusPagination } from './use-issue-status-branches';
import { useIssueGroupBranches, type IssueGroupBranches } from './use-issue-group-branches';

interface UseIssueSurfaceControllerInput {
  scope: IssueScope;
  modes: IssueSurfaceMode[];
  createDefaults?: IssueCreateDefaults;
  search?: string;
}

export interface IssueSurfaceController {
  scopeKey: string;
  projectId?: string;
  createDefaults: IssueCreateDefaults;
  viewMode: IssueSurfaceMode;
  allowGantt: boolean;
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
  sort: IssueSortParam;
  ganttIssues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  statusPagination?: IssueStatusPagination;
  groupBranches?: IssueGroupBranches;
  activeFilters: Omit<IssueFilters, 'statusFilters'>;
  actions: IssueSurfaceActions;
  selection: IssueSurfaceSelection;
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  resolveTableExportLookups: (needs: { projects: boolean; childProgress: boolean }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
  tableSearch: string;
  tableQuerySpec: IssueTableQuerySpec;
  tableFacetCounts?: IssueTableFacetsResponse;
  facetCountsExact: boolean;
  setActiveTableFacet: (facet: IssueTableFacetSpec | null) => void;
  setTableSearch: (query: string) => void;
  exportTableIssues: () => Promise<Issue[]>;
  isLoading: boolean;
  isRefreshing: boolean;
  isEmpty: boolean;
  openCreateIssue: (defaults?: IssueCreateDefaults) => void;
  moveIssue: (issueId: string, updates: MoveIssueUpdates, onSettled?: () => void) => void;
}

function issueDateFilterToApiParams(filter: IssueDateFilter | null) {
  if (!filter) return {};

  const from = dateOnlyToLocalDate(filter.from);
  const to = dateOnlyToLocalDate(filter.to);
  if (!from || !to) return {};

  const start = from <= to ? from : to;
  const endSource = from <= to ? to : from;
  const end = new Date(endSource);
  end.setDate(end.getDate() + 1);

  return {
    date_field: filter.field,
    date_start: start.toISOString(),
    date_end: end.toISOString(),
  };
}

function useDebouncedTableSearch(value: string, delayMs = 250) {
  const [debouncedValue, setDebouncedValue] = useState(value.trim());

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedValue(value.trim()), delayMs);
    return () => window.clearTimeout(timer);
  }, [delayMs, value]);

  return debouncedValue;
}

export function useIssueSurfaceController({
  scope,
  modes,
  createDefaults,
  search = '',
}: UseIssueSurfaceControllerInput): IssueSurfaceController {
  const wsId = useWorkspaceId();
  const queryPlan = useMemo<IssueSurfaceQueryPlan>(
    () => buildIssueSurfaceQueryPlan(scope),
    [scope],
  );
  const scopeKey = queryPlan.scopeKey;
  const projectId = scope.type === 'project' ? scope.projectId : undefined;

  const viewMode = useViewStore((s) => s.viewMode);
  const setViewMode = useViewStore((s) => s.setViewMode);
  const grouping = useViewStore((s) => s.grouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const dateFilter = useViewStore((s) => s.dateFilter);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const showSubIssues = useViewStore((s) => s.showSubIssues);
  const ganttShowCompleted = useViewStore((s) => s.ganttShowCompleted);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const tableColumns = useViewStore((s) => s.tableColumns);
  const listCollapsedStatuses = useViewStore((s) => s.listCollapsedStatuses);
  const [tableSearch, setTableSearch] = useState('');

  const allowedModes = useMemo(() => new Set<IssueSurfaceMode>(modes), [modes]);
  const fallbackMode = modes[0] ?? 'list';
  const effectiveViewMode = allowedModes.has(viewMode as IssueSurfaceMode)
    ? (viewMode as IssueSurfaceMode)
    : fallbackMode;

  useEffect(() => {
    if (!allowedModes.has(viewMode as IssueSurfaceMode)) {
      setViewMode(fallbackMode);
    }
  }, [allowedModes, fallbackMode, setViewMode, viewMode]);

  const resolvedCreateDefaults = useMemo(
    () => ({ ...queryPlan.createDefaults, ...createDefaults }),
    [createDefaults, queryPlan.createDefaults],
  );

  const dateParams = useMemo(() => issueDateFilterToApiParams(dateFilter), [dateFilter]);
  const { data: workspaceProperties = [], isSuccess: catalogSettled } = useQuery(
    propertyListOptions(wsId),
  );
  const activePropertyIds = useMemo(
    () => new Set(workspaceProperties.map((p) => p.id)),
    [workspaceProperties],
  );
  const effectivePropertyFilters = useMemo(() => {
    if (!catalogSettled) return propertyFilters;
    const entries = Object.entries(propertyFilters).filter(
      ([propertyId, selected]) => selected.length > 0 && activePropertyIds.has(propertyId),
    );
    if (entries.length === Object.keys(propertyFilters).length) return propertyFilters;
    return Object.fromEntries(entries);
  }, [activePropertyIds, catalogSettled, propertyFilters]);

  const rawPropertySortId = propertyIdFromViewKey(sortBy);
  const propertySortId =
    rawPropertySortId && (!catalogSettled || activePropertyIds.has(rawPropertySortId))
      ? rawPropertySortId
      : null;
  const sort = useMemo<IssueSortParam>(() => {
    const sortBy_: IssueSortParam['sort_by'] = propertySortId
      ? `property:${propertySortId}`
      : rawPropertySortId
        ? 'position'
        : (sortBy as Exclude<SortField, `property:${string}`>);
    return {
      sort_by: sortBy_,
      sort_direction: sortBy_ !== 'position' ? sortDirection : undefined,
      ...dateParams,
      ...(Object.keys(effectivePropertyFilters).length > 0
        ? { properties: effectivePropertyFilters }
        : {}),
    };
  }, [
    dateParams,
    effectivePropertyFilters,
    propertySortId,
    rawPropertySortId,
    sortBy,
    sortDirection,
  ]);

  const groupingPropertyId = propertyIdFromViewKey(grouping);
  const activeGroupingProperty = groupingPropertyId
    ? (workspaceProperties.find(
        (property) => property.id === groupingPropertyId && property.type === 'select',
      ) ?? null)
    : null;
  const effectiveGrouping =
    groupingPropertyId && catalogSettled && !activeGroupingProperty ? 'status' : grouping;
  const usesAssigneeBoard = effectiveViewMode === 'board' && effectiveGrouping === 'assignee';
  const usesGantt = effectiveViewMode === 'gantt' && !!projectId;
  const usesTable = effectiveViewMode === 'table';
  const activeSearch = usesTable ? tableSearch : search;
  const debouncedActiveSearch = useDebouncedTableSearch(activeSearch);
  const usesServerStatusSurface =
    effectiveViewMode === 'list' ||
    (effectiveViewMode === 'board' && effectiveGrouping === 'status');
  const usesServerGroupSurface =
    (effectiveViewMode === 'board' && effectiveGrouping !== 'status') ||
    effectiveViewMode === 'swimlane';
  const usesServerFacets = usesTable || usesServerStatusSurface || usesServerGroupSurface;
  const serverStatuses = useMemo<IssueStatus[]>(() => {
    const visible =
      statusFilters.length > 0
        ? ALL_STATUSES.filter((status) => statusFilters.includes(status))
        : [...ALL_STATUSES];
    return effectiveViewMode === 'list'
      ? visible.filter((status) => !listCollapsedStatuses.includes(status))
      : visible;
  }, [effectiveViewMode, listCollapsedStatuses, statusFilters]);

  const projectFilterState = useMemo(
    () => ({
      projectFilters: scope.type === 'project' ? [] : projectFilters,
      includeNoProject: scope.type === 'project' ? false : includeNoProject,
    }),
    [includeNoProject, projectFilters, scope.type],
  );
  const { projectFilters: viewProjectFilters, includeNoProject: viewIncludeNoProject } =
    projectFilterState;

  const workingAgentMineRelation =
    scope.type === 'my' ? (scope.relation === 'all' ? 'any' : scope.relation) : undefined;
  const backgroundPollingInterval = useRealtimePollingInterval(
    BACKGROUND_DEGRADED_POLL_INTERVAL_MS,
  );
  const { data: workspaceWorkingAgents = [] } = useQuery({
    ...workspaceWorkingAgentsOptions(wsId, 'issue', workingAgentMineRelation),
    refetchInterval: backgroundPollingInterval,
  });
  const workingIssueIDs = useMemo(() => {
    const issueIDs = new Set<string>();
    for (const agent of workspaceWorkingAgents) {
      for (const issueID of agent.issue_ids) issueIDs.add(issueID);
    }
    return issueIDs;
  }, [workspaceWorkingAgents]);

  const tableQuerySpec = useMemo<IssueTableQuerySpec>(() => {
    let queryScope: IssueTableQuerySpec['scope'];
    switch (scope.type) {
      case 'workspace':
        queryScope = {
          kind: 'workspace',
          ...(scope.actorKind === 'members'
            ? { assignee_types: ['member' as const] }
            : scope.actorKind === 'agents'
              ? { assignee_types: ['agent' as const, 'squad' as const] }
              : {}),
        };
        break;
      case 'project':
        queryScope = { kind: 'project', project_id: scope.projectId };
        break;
      case 'my':
        queryScope = {
          kind: 'my',
          relation: scope.relation === 'all' ? 'any' : scope.relation,
        };
        break;
      case 'actor':
        queryScope = {
          kind: scope.relation === 'assigned' ? 'assignee' : 'creator',
          actor: { type: scope.actorType, id: scope.actorId },
        };
        break;
      case 'team':
        throw new Error('Team issue scope is not supported by the Table query');
    }

    const date =
      dateParams.date_field && dateParams.date_start && dateParams.date_end
        ? {
            field: dateParams.date_field,
            start: dateParams.date_start,
            end: dateParams.date_end,
          }
        : undefined;
    return {
      scope: queryScope,
      filters: {
        ...(statusFilters.length > 0 ? { statuses: statusFilters } : {}),
        ...(priorityFilters.length > 0 ? { priorities: priorityFilters } : {}),
        ...(assigneeFilters.length > 0 ? { assignees: assigneeFilters } : {}),
        ...(includeNoAssignee ? { include_no_assignee: true } : {}),
        ...(creatorFilters.length > 0 ? { creators: creatorFilters } : {}),
        ...(viewProjectFilters.length > 0 ? { project_ids: viewProjectFilters } : {}),
        ...(viewIncludeNoProject ? { include_no_project: true } : {}),
        ...(labelFilters.length > 0 ? { label_ids: labelFilters } : {}),
        ...(Object.keys(effectivePropertyFilters).length > 0
          ? { properties: effectivePropertyFilters }
          : {}),
        ...(date ? { date } : {}),
        ...(agentRunningFilter ? { working_issue_ids: [...workingIssueIDs] } : {}),
        include_sub_issues: showSubIssues,
      },
      ...(debouncedActiveSearch ? { search: debouncedActiveSearch } : {}),
      sort: {
        field: sort.sort_by ?? 'position',
        direction: sort.sort_direction ?? 'asc',
      },
    };
  }, [
    agentRunningFilter,
    assigneeFilters,
    creatorFilters,
    dateParams,
    debouncedActiveSearch,
    effectivePropertyFilters,
    includeNoAssignee,
    labelFilters,
    priorityFilters,
    scope,
    showSubIssues,
    sort.sort_by,
    sort.sort_direction,
    statusFilters,
    viewIncludeNoProject,
    viewProjectFilters,
    workingIssueIDs,
  ]);

  const [activeTableFacet, setActiveTableFacet] = useState<IssueTableFacetSpec | null>(null);
  const requestedFacets = useMemo<IssueTableFacetSpec[]>(() => {
    const facets: IssueTableFacetSpec[] = [];
    if (usesServerStatusSurface) facets.push({ kind: 'status' });
    if (
      activeTableFacet &&
      !facets.some(
        (facet) =>
          facet.kind === activeTableFacet.kind &&
          (facet.kind !== 'property' ||
            activeTableFacet.kind !== 'property' ||
            facet.property_id === activeTableFacet.property_id),
      )
    ) {
      facets.push(activeTableFacet);
    }
    return facets.length > 0 ? facets : [{ kind: 'status' }];
  }, [activeTableFacet, usesServerStatusSurface]);
  const tableFacetRequest = useMemo(
    () => ({
      query: tableQuerySpec,
      facets: requestedFacets,
      include_total: usesServerStatusSurface,
    }),
    [requestedFacets, tableQuerySpec, usesServerStatusSurface],
  );
  const tableFacetsQuery = useQuery({
    ...issueTableFacetsOptions(wsId, tableFacetRequest),
    placeholderData: keepPreviousData,
    enabled:
      usesServerStatusSurface ||
      ((usesTable || usesServerGroupSurface) && activeTableFacet !== null),
    refetchInterval: backgroundPollingInterval,
  });
  useEffect(() => {
    if (!usesServerFacets) setActiveTableFacet(null);
  }, [usesServerFacets]);
  const requestActiveTableFacet = useCallback(
    (facet: IssueTableFacetSpec | null) => {
      setActiveTableFacet(usesServerFacets ? facet : null);
    },
    [usesServerFacets],
  );
  const serverStatusBranches = useIssueStatusBranches({
    wsId,
    query: tableQuerySpec,
    statuses: serverStatuses,
    facets: tableFacetsQuery.data,
    facetsPending: tableFacetsQuery.isPending,
    facetsFetching: tableFacetsQuery.isFetching,
    enabled: usesServerStatusSurface,
  });
  const serverGroupSpec = useMemo<IssueTableGroupsRequest['group']>(() => {
    if (effectiveViewMode === 'swimlane') {
      return {
        kind: 'compound',
        primary: swimlaneGrouping,
        secondary: 'status',
        secondary_values: serverStatuses,
      };
    }
    const propertyId = propertyIdFromViewKey(effectiveGrouping);
    if (propertyId) {
      return {
        kind: 'property',
        property_id: propertyId,
        include_empty: true,
      };
    }
    return { kind: 'assignee' };
  }, [effectiveGrouping, effectiveViewMode, serverStatuses, swimlaneGrouping]);
  const serverGroupQuery = useMemo<IssueTableQuerySpec>(() => {
    if (effectiveViewMode !== 'swimlane') return tableQuerySpec;
    const { statuses: _statuses, ...filters } = tableQuerySpec.filters;
    return { ...tableQuerySpec, filters };
  }, [effectiveViewMode, tableQuerySpec]);
  const serverGroupBranches = useIssueGroupBranches({
    wsId,
    query: serverGroupQuery,
    group: serverGroupSpec,
    secondaryValues: effectiveViewMode === 'swimlane' ? serverStatuses : undefined,
    observeEmptyBranches:
      effectiveViewMode === 'swimlane' ||
      (effectiveViewMode === 'board' && activeGroupingProperty !== null),
    enabled: usesServerGroupSurface,
  });

  const membershipKey = useMemo(
    () =>
      JSON.stringify([
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        viewProjectFilters,
        viewIncludeNoProject,
        labelFilters,
        effectivePropertyFilters,
        agentRunningFilter,
        showSubIssues,
        dateParams,
        debouncedActiveSearch,
      ]),
    [
      agentRunningFilter,
      assigneeFilters,
      creatorFilters,
      dateParams,
      debouncedActiveSearch,
      effectivePropertyFilters,
      includeNoAssignee,
      labelFilters,
      priorityFilters,
      showSubIssues,
      statusFilters,
      viewIncludeNoProject,
      viewProjectFilters,
    ],
  );
  const selection = useCreateIssueSurfaceSelection(
    scopeKey,
    `${scopeKey}:${effectiveViewMode}:${membershipKey}`,
  );

  const projectsAreLaneSet = effectiveViewMode === 'swimlane' && swimlaneGrouping === 'project';

  const data = useIssueSurfaceData({
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
    projectFilters: viewProjectFilters,
    includeNoProject: viewIncludeNoProject,
    labelFilters,
    propertyFilters: effectivePropertyFilters,
    workingIssueIDs,
    showSubIssues,
    loadProjects:
      cardProperties.project ||
      (usesTable && tableColumns.some((column) => column.key === 'project')) ||
      projectsAreLaneSet,
    projectsAreLaneSet,
  });

  const exportTableIssues = useCallback(async () => {
    const issues: Issue[] = [];
    const seenIssueIds = new Set<string>();
    const seenCursors = new Set<string>();
    let fingerprint: string | null = null;
    let expectedTotal: number | null = null;
    let cursor: string | null = null;
    do {
      if (cursor !== null) {
        if (seenCursors.has(cursor)) throw new IssueTableExportIntegrityError();
        seenCursors.add(cursor);
      }
      const page = await api.listIssueTableRows({
        query: tableQuerySpec,
        group: { kind: 'none' },
        group_key: null,
        hierarchy: { enabled: false },
        parent_id: null,
        page: { limit: 100, cursor },
      });
      if (!page.query_fingerprint) throw new IssueTableExportIntegrityError();
      fingerprint ??= page.query_fingerprint;
      if (cursor === null) expectedTotal = page.total;
      if (
        page.query_fingerprint !== fingerprint ||
        page.group_key !== null ||
        page.parent_id !== null
      ) {
        throw new IssueTableExportIntegrityError();
      }
      for (const row of page.rows) {
        if (seenIssueIds.has(row.issue.id)) {
          throw new IssueTableExportIntegrityError();
        }
        seenIssueIds.add(row.issue.id);
        issues.push(row.issue);
      }
      cursor = page.next_cursor;
    } while (cursor);
    if (issues.length !== (expectedTotal ?? 0)) {
      throw new IssueTableExportIntegrityError();
    }
    return issues;
  }, [tableQuerySpec]);

  const { actions, openCreateIssue, moveIssue } = useIssueSurfaceActions({
    createDefaults: resolvedCreateDefaults,
  });

  return {
    scopeKey,
    projectId,
    createDefaults: resolvedCreateDefaults,
    viewMode: effectiveViewMode,
    allowGantt: allowedModes.has('gantt') && !!projectId,
    ...data,
    statusPagination: usesServerStatusSurface ? data.statusPagination : undefined,
    groupBranches: usesServerGroupSurface ? serverGroupBranches : undefined,
    isEmpty:
      data.isEmpty &&
      !data.isRefreshing &&
      !(usesTable && (tableSearch.trim() || debouncedActiveSearch)),
    sort,
    actions,
    selection,
    tableSearch,
    tableQuerySpec,
    tableFacetCounts:
      usesServerStatusSurface ||
      ((usesTable || usesServerGroupSurface) && activeTableFacet !== null)
        ? tableFacetsQuery.data
        : undefined,
    facetCountsExact: !usesTable && !usesServerStatusSurface && !usesServerGroupSurface,
    setActiveTableFacet: requestActiveTableFacet,
    setTableSearch,
    openCreateIssue,
    moveIssue,
    exportTableIssues,
  };
}
