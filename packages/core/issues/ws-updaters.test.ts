import { beforeEach, describe, expect, it } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from '../agents/queries';
import {
  onIssueCreated,
  onIssueDeleted,
  onIssueLabelsChanged,
  onIssueMetadataChanged,
  onIssuePropertiesChanged,
  onIssueUpdated,
  patchIssueLabels,
  patchIssueProperties,
} from './ws-updaters';
import { issueKeys } from './queries';
import { labelKeys } from '../labels/queries';
import { projectKeys } from '../projects/queries';
import type {
  AgentActivityBucket,
  AgentRunCount,
  AgentTask,
  Attachment,
  Issue,
  IssueReaction,
  IssueLabelsResponse,
  IssueTableRowsResponse,
  IssueSubscriber,
  IssueUsageSummary,
  Label,
  ListIssuesCache,
  TimelineEntry,
} from '../types';

const WS_ID = 'ws-1';
const ISSUE_ID = 'issue-1';
const OTHER_ISSUE_ID = 'issue-2';
const PARENT_ISSUE_ID = 'parent-1';
const AGENT_ID = 'agent-1';
const PROJECT_ID = 'project-1';

const labelA: Label = {
  id: 'label-a',
  workspace_id: WS_ID,
  name: 'bug',
  color: '#ef4444',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
};

const labelB: Label = {
  id: 'label-b',
  workspace_id: WS_ID,
  name: 'feature',
  color: '#22c55e',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
};

const baseIssue: Issue = {
  id: ISSUE_ID,
  workspace_id: WS_ID,
  number: 1,
  identifier: 'MUL-1',
  title: 'Test',
  description: null,
  status: 'todo',
  priority: 'none',
  assignee_type: null,
  assignee_id: null,
  creator_type: 'member',
  creator_id: 'user-1',
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  labels: [labelA],
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
};

const parentedIssue: Issue = {
  ...baseIssue,
  parent_issue_id: PARENT_ISSUE_ID,
};

const otherIssue: Issue = {
  ...baseIssue,
  id: OTHER_ISSUE_ID,
  identifier: 'MUL-2',
  title: 'Other',
};

function makeListCache(...issues: Issue[]): ListIssuesCache {
  return {
    byStatus: {
      todo: { issues, total: issues.length },
    },
  };
}

const tableRowKey = [
  ...issueKeys.tableRows(
    WS_ID,
    {
      scope: { kind: 'workspace' },
      filters: {},
      sort: { field: 'position', direction: 'asc' },
    },
    { kind: 'none' },
    null,
    false,
    null,
  ),
  'page',
  null,
] as const;

function seedTableRow(qc: QueryClient, issue = baseIssue) {
  qc.setQueryData<IssueTableRowsResponse>(tableRowKey, {
    query_fingerprint: 'sha256:table',
    group_key: null,
    parent_id: null,
    total: 1,
    rows: [{ issue, direct_child_count: 0 }],
    branch_total: 1,
    next_cursor: null,
  });
}

function makeTask(issueId = ISSUE_ID): AgentTask {
  return {
    id: `task-${issueId}`,
    agent_id: AGENT_ID,
    runtime_id: 'runtime-1',
    issue_id: issueId,
    status: 'completed',
    priority: 0,
    dispatched_at: null,
    started_at: '2025-01-01T00:00:00Z',
    completed_at: '2025-01-01T00:01:00Z',
    result: null,
    error: null,
    created_at: '2025-01-01T00:00:00Z',
  };
}

function expectInvalidated(qc: QueryClient, queryKey: readonly unknown[]) {
  expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
}

describe('onIssueLabelsChanged', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it('patches the per-issue label cache when present (LabelPicker source)', () => {
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID), {
      labels: [labelA],
    });

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    expect(qc.getQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID))).toEqual({
      labels: [labelB],
    });
  });

  it('leaves the per-issue label cache untouched when the picker has not fetched', () => {
    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, ISSUE_ID))).toBeUndefined();
  });

  it('still patches the list, Table row, and detail caches', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: { todo: { issues: [baseIssue], total: 1 } },
    });
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    seedTableRow(qc);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.todo?.issues[0]?.labels).toEqual([labelB]);

    const detail = qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID));
    expect(detail?.labels).toEqual([labelB]);
    expect(qc.getQueryData<IssueTableRowsResponse>(tableRowKey)?.rows[0]?.issue.labels).toEqual([
      labelB,
    ]);
  });

  it('patches the Project Gantt cache so label filters react in place', () => {
    const PROJECT_ID = 'project-1';
    qc.setQueryData<Issue[]>(issueKeys.projectGantt(WS_ID, PROJECT_ID), [baseIssue, otherIssue]);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    const gantt = qc.getQueryData<Issue[]>(issueKeys.projectGantt(WS_ID, PROJECT_ID));
    expect(gantt?.find((i) => i.id === ISSUE_ID)?.labels).toEqual([labelB]);
    expect(gantt?.find((i) => i.id === OTHER_ISSUE_ID)?.labels).toEqual([labelA]);
  });

  it('defers label-filtered flat-window invalidation until commit', () => {
    const flatKey = issueKeys.flat(
      WS_ID,
      'workspace:all',
      { label_ids: [labelB.id] },
      { sort_by: 'position' },
    );
    qc.setQueryData(flatKey, {
      pages: [{ issues: [baseIssue], total: 1 }],
      pageParams: [0],
    });

    patchIssueLabels(qc, WS_ID, ISSUE_ID, [labelB]);
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);
    expectInvalidated(qc, flatKey);
  });

  it("patches the parent's children cache so the sub-issues panel stays fresh", () => {
    const child = { ...baseIssue, parent_issue_id: PARENT_ISSUE_ID };
    const childrenKey = issueKeys.children(WS_ID, PARENT_ISSUE_ID);
    qc.setQueryData<Issue[]>(childrenKey, [child, otherIssue]);
    const unrelated = [otherIssue];
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, 'parent-9'), unrelated);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    const children = qc.getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expect(children?.find((i) => i.id === ISSUE_ID)?.labels).toEqual([labelB]);
    expect(children?.find((i) => i.id === OTHER_ISSUE_ID)?.labels).toEqual([labelA]);
    expect(qc.getQueryData<Issue[]>(issueKeys.children(WS_ID, 'parent-9'))).toBe(unrelated);
    expectInvalidated(qc, childrenKey);
  });

  it('invalidates batched children caches (Map-shaped, not patchable)', () => {
    const batchedKey = issueKeys.childrenByParents(WS_ID, [PARENT_ISSUE_ID]);
    qc.setQueryData(batchedKey, new Map([[PARENT_ISSUE_ID, [baseIssue]]]));

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    expectInvalidated(qc, batchedKey);
  });
});

describe('onIssueMetadataChanged', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it('replaces metadata in both detail and list caches (no merge)', () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), {
      ...baseIssue,
      metadata: { pr_number: 1, stale: 'yes' },
    });
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        todo: {
          issues: [{ ...baseIssue, metadata: { pr_number: 1 } }],
          total: 1,
        },
      },
    });
    seedTableRow(qc, {
      ...baseIssue,
      metadata: { pr_number: 1, stale: 'yes' },
    });

    onIssueMetadataChanged(qc, WS_ID, ISSUE_ID, { pr_number: 2 });

    const detail = qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID));
    expect(detail?.metadata).toEqual({ pr_number: 2 });
    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.todo?.issues[0]?.metadata).toEqual({ pr_number: 2 });
    expect(qc.getQueryData<IssueTableRowsResponse>(tableRowKey)?.rows[0]?.issue.metadata).toEqual({
      pr_number: 2,
    });
  });

  it('leaves untouched caches as undefined (no spurious writes)', () => {
    onIssueMetadataChanged(qc, WS_ID, ISSUE_ID, { foo: 'bar' });

    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.list(WS_ID))).toBeUndefined();
  });

  it('re-sorts an updated_at-sorted board but not a position-sorted one', () => {
    const boardUpdatedKey = issueKeys.listSorted(WS_ID, {
      sort_by: 'updated_at',
      sort_direction: 'desc',
    });
    const boardPositionKey = issueKeys.listSorted(WS_ID, { sort_by: 'position' });
    qc.setQueryData<ListIssuesCache>(boardUpdatedKey, makeListCache(baseIssue));
    qc.setQueryData<ListIssuesCache>(boardPositionKey, makeListCache(baseIssue));

    onIssueMetadataChanged(qc, WS_ID, ISSUE_ID, { foo: 'bar' });

    expectInvalidated(qc, boardUpdatedKey);
    expect(qc.getQueryState(boardPositionKey)?.isInvalidated).toBe(false);
  });
});

describe('issue property snapshots', () => {
  it('patches per-parent children and invalidates every children projection on commit', () => {
    const qc = new QueryClient();
    const childrenKey = issueKeys.children(WS_ID, PARENT_ISSUE_ID);
    const unrelatedKey = issueKeys.children(WS_ID, 'parent-9');
    const batchedKey = issueKeys.childrenByParents(WS_ID, [PARENT_ISSUE_ID]);
    const child = {
      ...parentedIssue,
      properties: { estimate: 1, environment: 'staging' },
    };
    const unrelated = [otherIssue];
    qc.setQueryData<Issue[]>(childrenKey, [child, otherIssue]);
    qc.setQueryData<Issue[]>(unrelatedKey, unrelated);
    qc.setQueryData(batchedKey, new Map([[PARENT_ISSUE_ID, [child]]]));

    patchIssueProperties(qc, WS_ID, ISSUE_ID, {
      estimate: 2,
      environment: 'staging',
    });

    expect(
      qc.getQueryData<Issue[]>(childrenKey)?.find((candidate) => candidate.id === ISSUE_ID)
        ?.properties,
    ).toEqual({ estimate: 2, environment: 'staging' });
    expect(qc.getQueryState(childrenKey)?.isInvalidated).toBe(false);
    expect(qc.getQueryData<Issue[]>(unrelatedKey)).toBe(unrelated);

    onIssuePropertiesChanged(qc, WS_ID, ISSUE_ID, {
      estimate: 3,
      environment: 'staging',
    });

    expect(
      qc.getQueryData<Issue[]>(childrenKey)?.find((candidate) => candidate.id === ISSUE_ID)
        ?.properties,
    ).toEqual({ estimate: 3, environment: 'staging' });
    expectInvalidated(qc, childrenKey);
    expectInvalidated(qc, batchedKey);
    expect(qc.getQueryData<Issue[]>(unrelatedKey)).toBe(unrelated);
  });

  it('keeps optimistic patches local, then invalidates property windows after commit', () => {
    const qc = new QueryClient();
    const flatKey = issueKeys.flat(
      WS_ID,
      'workspace:all',
      {},
      { sort_by: 'property:estimate', properties: { estimate: ['3'] } },
    );
    qc.setQueryData(flatKey, {
      pages: [{ issues: [baseIssue], total: 1 }],
      pageParams: [0],
    });
    seedTableRow(qc);

    patchIssueProperties(qc, WS_ID, ISSUE_ID, { estimate: 3 });

    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);
    expect(
      qc.getQueryData<{ pages: { issues: Issue[] }[] }>(flatKey)?.pages[0]?.issues[0]?.properties,
    ).toEqual({ estimate: 3 });
    expect(qc.getQueryData<IssueTableRowsResponse>(tableRowKey)?.rows[0]?.issue.properties).toEqual(
      { estimate: 3 },
    );

    onIssuePropertiesChanged(qc, WS_ID, ISSUE_ID, { estimate: 4 });

    expectInvalidated(qc, flatKey);
  });

  it('re-sorts an updated_at-sorted board but not a position-sorted one after commit', () => {
    const qc = new QueryClient();
    const boardUpdatedKey = issueKeys.listSorted(WS_ID, {
      sort_by: 'updated_at',
      sort_direction: 'desc',
    });
    const boardPositionKey = issueKeys.listSorted(WS_ID, { sort_by: 'position' });
    qc.setQueryData<ListIssuesCache>(boardUpdatedKey, makeListCache(baseIssue));
    qc.setQueryData<ListIssuesCache>(boardPositionKey, makeListCache(baseIssue));

    patchIssueProperties(qc, WS_ID, ISSUE_ID, { estimate: 3 });
    expect(qc.getQueryState(boardUpdatedKey)?.isInvalidated).toBe(false);

    onIssuePropertiesChanged(qc, WS_ID, ISSUE_ID, { estimate: 4 });

    expectInvalidated(qc, boardUpdatedKey);
    expect(qc.getQueryState(boardPositionKey)?.isInvalidated).toBe(false);
  });
});

describe('project progress invalidation', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData(projectKeys.list(WS_ID), [
      {
        id: PROJECT_ID,
        workspace_id: WS_ID,
        title: 'Project',
        description: null,
        icon: null,
        status: 'in_progress',
        priority: 'none',
        lead_type: null,
        lead_id: null,
        issue_count: 1,
        done_count: 0,
        resource_count: 0,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ]);
  });

  it('invalidates project queries when an issue status changes', () => {
    onIssueUpdated(qc, WS_ID, {
      id: ISSUE_ID,
      status: 'done',
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });

  it('invalidates project queries when a project issue is created', () => {
    onIssueCreated(qc, WS_ID, {
      ...baseIssue,
      project_id: PROJECT_ID,
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });
});

describe('onIssueCreated — carries the label snapshot into list cache', () => {
  it("keeps the created issue's labels so members other than the creator render it already labeled", () => {
    const qc = new QueryClient();
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache());

    onIssueCreated(qc, WS_ID, { ...baseIssue, labels: [labelA, labelB] });

    const cache = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    const cached = cache?.byStatus.todo?.issues.find((i) => i.id === ISSUE_ID);
    expect(cached?.labels).toEqual([labelA, labelB]);
  });
});

describe('onIssueUpdated — position move is surgical, not a list refetch', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  const issueA: Issue = { ...baseIssue, id: 'issue-1', position: 0 };
  const issueB: Issue = { ...baseIssue, id: 'issue-2', position: 10 };

  it('reorders the moved card in place and does NOT invalidate the workspace list', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache(issueA, issueB));

    onIssueUpdated(qc, WS_ID, { ...issueA, position: 20 });

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.todo?.issues.map((i) => i.id)).toEqual(['issue-2', 'issue-1']);
    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it('surgically patches the filtered myAll lists on a non-membership change (no refetch)', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache(issueA, issueB));
    qc.setQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID), makeListCache(issueA, issueB));

    onIssueUpdated(qc, WS_ID, { ...issueA, position: 20 });

    const my = qc.getQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID));
    expect(my?.byStatus.todo?.issues.map((i) => i.id)).toEqual(['issue-2', 'issue-1']);
    expect(qc.getQueryState(issueKeys.myAll(WS_ID))?.isInvalidated).toBe(false);
  });

  it('removes the card from an assignee-filtered list when the assignee changes (membership-aware, no blanket refetch)', () => {
    const assignedKey = issueKeys.myListSorted(
      WS_ID,
      'assigned',
      { assignee_id: 'user-1' },
      undefined,
    );
    const mine: Issue = { ...issueA, assignee_type: 'member', assignee_id: 'user-1' };
    qc.setQueryData<ListIssuesCache>(assignedKey, makeListCache(mine));

    onIssueUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: 'member', assignee_id: 'user-2' },
      { assigneeChanged: true },
    );

    const list = qc.getQueryData<ListIssuesCache>(assignedKey);
    expect(list?.byStatus.todo?.issues).toEqual([]);
    expect(list?.byStatus.todo?.total).toBe(0);
    expect(qc.getQueryState(assignedKey)?.isInvalidated).toBe(false);
  });

  it('flags union-scope (my:all) lists stale on an assignee change instead of guessing membership', () => {
    const myAllListKey = issueKeys.myListSorted(WS_ID, 'all', {}, undefined);
    const mine: Issue = { ...issueA, assignee_type: 'member', assignee_id: 'user-1' };
    qc.setQueryData<ListIssuesCache>(myAllListKey, makeListCache(mine));

    onIssueUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: 'member', assignee_id: 'user-2' },
      { assigneeChanged: true },
    );

    const list = qc.getQueryData<ListIssuesCache>(myAllListKey);
    expect(list?.byStatus.todo?.issues[0]?.assignee_id).toBe('user-2');
    expectInvalidated(qc, myAllListKey);
  });

  it("moves the card out of the old project's list and flags the loaded target list (legacy diff fallback, no server flag)", () => {
    const targetKey = issueKeys.myListSorted(
      WS_ID,
      'project:project-9',
      { project_id: 'project-9' },
      undefined,
    );
    qc.setQueryData<ListIssuesCache>(targetKey, makeListCache());

    onIssueUpdated(qc, WS_ID, { ...issueA, project_id: 'project-9' });

    expect(qc.getQueryData<ListIssuesCache>(targetKey)?.byStatus.todo?.issues).toEqual([]);
    expectInvalidated(qc, targetKey);
  });

  it("drops the card from the old project's list on a server project_changed flag even when the cached project_id already matches", () => {
    const moved: Issue = { ...issueA, project_id: 'project-9' };
    const oldProjectKey = issueKeys.myListSorted(
      WS_ID,
      'project:project-1',
      { project_id: 'project-1' },
      undefined,
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, moved.id), moved);
    qc.setQueryData<ListIssuesCache>(oldProjectKey, makeListCache(moved));

    onIssueUpdated(qc, WS_ID, moved, { projectChanged: true });

    expect(qc.getQueryData<ListIssuesCache>(oldProjectKey)?.byStatus.todo?.issues).toEqual([]);
  });

  it('does NOT touch project lists when the server flag says project_changed=false (flag overrides the legacy diff)', () => {
    const projectKey = issueKeys.myListSorted(
      WS_ID,
      'project:project-9',
      { project_id: 'project-9' },
      undefined,
    );
    qc.setQueryData<ListIssuesCache>(projectKey, makeListCache());

    onIssueUpdated(qc, WS_ID, { ...issueA, project_id: 'project-9' }, { projectChanged: false });

    expect(qc.getQueryState(projectKey)?.isInvalidated).toBe(false);
  });
});

describe('onIssueUpdated — off-screen status change reconciles column counts', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it('refetches the workspace list when a status-changed issue is not in the loaded page', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        in_review: { issues: [], total: 1 },
        done: { issues: [], total: 60 },
      },
    });

    onIssueUpdated(qc, WS_ID, { id: 'off-screen', status: 'done' }, { statusChanged: true });

    expectInvalidated(qc, issueKeys.list(WS_ID));
  });

  it('refetches the filtered myAll list under the same condition', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID), {
      byStatus: { done: { issues: [], total: 60 } },
    });

    onIssueUpdated(qc, WS_ID, { id: 'off-screen', status: 'done' }, { statusChanged: true });

    expectInvalidated(qc, issueKeys.myAll(WS_ID));
  });

  it("moves the bucket counts AND refetches when the off-screen issue's base entity is known", () => {
    const offScreen: Issue = { ...baseIssue, id: 'off-screen', status: 'in_review' };
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, 'off-screen'), offScreen);
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        in_review: { issues: [], total: 1 },
        done: { issues: [], total: 60 },
      },
    });

    onIssueUpdated(qc, WS_ID, { ...offScreen, status: 'done' }, { statusChanged: true });

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.in_review?.total).toBe(0);
    expect(list?.byStatus.done?.total).toBe(61);
    expectInvalidated(qc, issueKeys.list(WS_ID));
  });

  it('does NOT refetch when the status-changed issue is loaded (surgical patch suffices)', () => {
    const loaded: Issue = { ...baseIssue, id: 'loaded', status: 'in_review' };
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        in_review: { issues: [loaded], total: 1 },
        done: { issues: [], total: 60 },
      },
    });

    onIssueUpdated(qc, WS_ID, { ...loaded, status: 'done' }, { statusChanged: true });

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.in_review?.total).toBe(0);
    expect(list?.byStatus.done?.total).toBe(61);
    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it('does NOT refetch an absent issue when the status did not change', () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: { done: { issues: [], total: 60 } },
    });

    onIssueUpdated(qc, WS_ID, { id: 'off-screen', title: 'renamed' });

    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });
});

describe('onIssueDeleted', () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it('removes every cache entry scoped directly to the deleted issue', () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID), [
      {
        type: 'activity',
        id: 'activity-1',
        actor_type: 'member',
        actor_id: 'user-1',
        action: 'created',
        created_at: '2025-01-01T00:00:00Z',
      },
    ]);
    qc.setQueryData<IssueReaction[]>(issueKeys.reactions(ISSUE_ID), [
      {
        id: 'reaction-1',
        issue_id: ISSUE_ID,
        actor_type: 'member',
        actor_id: 'user-1',
        emoji: '+1',
        created_at: '2025-01-01T00:00:00Z',
      },
    ]);
    qc.setQueryData<IssueSubscriber[]>(issueKeys.subscribers(ISSUE_ID), [
      {
        issue_id: ISSUE_ID,
        user_type: 'member',
        user_id: 'user-1',
        reason: 'manual',
        created_at: '2025-01-01T00:00:00Z',
      },
    ]);
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), {
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_read_tokens: 0,
      total_cache_write_tokens: 0,
      task_count: 1,
    });
    qc.setQueryData<Attachment[]>(issueKeys.attachments(ISSUE_ID), [
      {
        id: 'attachment-1',
        workspace_id: WS_ID,
        issue_id: ISSUE_ID,
        comment_id: null,
        chat_session_id: null,
        chat_message_id: null,
        uploader_type: 'member',
        uploader_id: 'user-1',
        filename: 'evidence.png',
        url: 's3://bucket/evidence.png',
        download_url: 'https://example.test/evidence.png',
        markdown_url: 'https://example.test/api/attachments/att-1/download',
        content_type: 'image/png',
        size_bytes: 1,
        created_at: '2025-01-01T00:00:00Z',
      },
    ]);
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(ISSUE_ID), [makeTask()]);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [otherIssue]);
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID), {
      labels: [labelA],
    });

    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, OTHER_ISSUE_ID), otherIssue);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(OTHER_ISSUE_ID), []);
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID), {
      labels: [labelB],
    });

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.timeline(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.reactions(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.subscribers(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.attachments(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.tasks(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, ISSUE_ID))).toBeUndefined();

    expect(qc.getQueryData(issueKeys.detail(WS_ID, OTHER_ISSUE_ID))).toEqual(otherIssue);
    expect(qc.getQueryData(issueKeys.timeline(OTHER_ISSUE_ID))).toEqual([]);
    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID))).toEqual({
      labels: [labelB],
    });
  });

  it('removes the deleted issue from workspace and my-issues list caches immediately', () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache(baseIssue, otherIssue));
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, 'assigned', myFilter),
      makeListCache(baseIssue, otherIssue),
    );

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    const myList = qc.getQueryData<ListIssuesCache>(issueKeys.myList(WS_ID, 'assigned', myFilter));
    expect(list?.byStatus.todo?.issues.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expect(list?.byStatus.todo?.total).toBe(1);
    expect(myList?.byStatus.todo?.issues.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expect(myList?.byStatus.todo?.total).toBe(1);
    expectInvalidated(qc, issueKeys.list(WS_ID));
    expectInvalidated(qc, issueKeys.myList(WS_ID, 'assigned', myFilter));
  });

  it('invalidates parent progress when the parent id only exists in detail cache', () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), parentedIssue);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [
      parentedIssue,
      otherIssue,
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const parentChildren = qc.getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it('invalidates parent progress when the deleted issue is only present in a children cache', () => {
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [
      parentedIssue,
      otherIssue,
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const parentChildren = qc.getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it('invalidates parent progress when the parent id only exists in a my-issues cache', () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, 'assigned', myFilter),
      makeListCache(parentedIssue, otherIssue),
    );
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [otherIssue]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const myList = qc.getQueryData<ListIssuesCache>(issueKeys.myList(WS_ID, 'assigned', myFilter));
    expect(myList?.byStatus.todo?.issues.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it('invalidates child progress when the deleted issue is itself a parent', () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [
      {
        ...otherIssue,
        parent_issue_id: ISSUE_ID,
      },
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map([[ISSUE_ID, { done: 0, total: 1 }]]));

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it('invalidates agent task and activity caches that can reference the deleted issue', () => {
    qc.setQueryData<AgentTask[]>(agentTaskSnapshotKeys.list(WS_ID), [makeTask()]);
    qc.setQueryData<AgentActivityBucket[]>(agentActivityKeys.last30d(WS_ID), [
      {
        agent_id: AGENT_ID,
        bucket_at: '2025-01-01T00:00:00Z',
        task_count: 1,
        failed_count: 0,
      },
    ]);
    qc.setQueryData<AgentRunCount[]>(agentRunCountsKeys.last30d(WS_ID), [
      { agent_id: AGENT_ID, run_count: 1 },
    ]);
    qc.setQueryData<AgentTask[]>(agentTasksKeys.detail(WS_ID, AGENT_ID), [makeTask()]);
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(ISSUE_ID), [makeTask()]);

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expectInvalidated(qc, agentTaskSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentActivityKeys.last30d(WS_ID));
    expectInvalidated(qc, agentRunCountsKeys.last30d(WS_ID));
    expectInvalidated(qc, agentTasksKeys.detail(WS_ID, AGENT_ID));
    expect(qc.getQueryData(issueKeys.tasks(ISSUE_ID))).toBeUndefined();
  });
});

describe('project gantt cache invalidation', () => {
  const PROJECT_ID = 'project-1';
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData<Issue[]>(issueKeys.projectGantt(WS_ID, PROJECT_ID), [baseIssue]);
  });

  it('invalidates the project Gantt cache on issue:created', () => {
    onIssueCreated(qc, WS_ID, otherIssue);
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it('invalidates the project Gantt cache on issue:updated', () => {
    onIssueUpdated(qc, WS_ID, {
      id: ISSUE_ID,
      start_date: '2026-01-01T00:00:00Z',
    });
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it('invalidates the project Gantt cache on issue:deleted', () => {
    onIssueDeleted(qc, WS_ID, ISSUE_ID);
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });
});
