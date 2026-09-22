// @vitest-environment jsdom

import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AgentTask, Issue } from '@goosar/core/types';
import { renderWithI18n } from '../../test/i18n';

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ 'agent-1': 'Niko', 'agent-2': 'J' })[id] ?? 'Unknown Agent',
    getActorInitials: (_type: string, id: string) =>
      ({ 'agent-1': 'NI', 'agent-2': 'J' })[id] ?? 'UA',
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock('@goosar/core/runtimes/queries', () => ({
  runtimeListOptions: () => ({ queryKey: ['runtimes'] }),
}));

vi.mock('@goosar/core/workspace/queries', () => ({
  agentListOptions: () => ({ queryKey: ['agents'] }),
}));

vi.mock('@goosar/core/agents', () => ({
  deriveAgentAvailability: () => 'online',
}));

vi.mock('@goosar/ui/components/common/actor-avatar', () => ({
  ActorAvatar: ({ name }: { name: string }) => <span data-testid="actor-avatar">{name}</span>,
}));

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return { ...actual, useQuery: () => ({ data: [] }) };
});

import {
  AgentActivityHoverContent,
  WorkspaceAgentActivityHoverContent,
} from './agent-activity-hover-content';

function makeIssue(id: string, identifier: string, title: string): Issue {
  return {
    id,
    workspace_id: 'ws-1',
    number: 1,
    identifier,
    title,
    description: null,
    status: 'in_progress',
    priority: 'none',
    assignee_type: null,
    assignee_id: null,
    creator_type: 'member',
    creator_id: 'user-1',
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: '2026-06-08T08:00:00Z',
    updated_at: '2026-06-08T08:00:00Z',
  };
}

function makeTask(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: 'task-1',
    agent_id: 'agent-1',
    runtime_id: 'runtime-1',
    issue_id: 'issue-1',
    status: 'running',
    priority: 0,
    dispatched_at: null,
    started_at: '2026-06-08T08:00:00Z',
    completed_at: null,
    result: null,
    error: null,
    created_at: '2026-06-08T08:00:00Z',
    ...overrides,
  };
}

afterEach(cleanup);

describe('AgentActivityHoverContent', () => {
  const threeTasksTwoAgents = [
    makeTask({ id: 't1', agent_id: 'agent-1' }),
    makeTask({ id: 't2', agent_id: 'agent-1' }),
    makeTask({ id: 't3', agent_id: 'agent-2' }),
  ];

  it('counts tasks, not agents, in the header', () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />);

    expect(screen.getByText('3 tasks working')).toBeInTheDocument();
    expect(screen.queryByText(/agents? working/)).not.toBeInTheDocument();
    expect(screen.getAllByTestId('actor-avatar')).toHaveLength(3);
  });

  it('uses the singular task copy for a single task', () => {
    renderWithI18n(<AgentActivityHoverContent tasks={[makeTask({})]} />);

    expect(screen.getByText('1 task working')).toBeInTheDocument();
  });

  it('renders the requested Chinese task copy', () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />, {
      locale: 'zh-Hans',
    });

    expect(screen.getByText('3 个 task 工作中')).toBeInTheDocument();
  });
});

describe('WorkspaceAgentActivityHoverContent', () => {
  it('carries the two figures the chip does not, and groups rows by issue', () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[
          makeIssue('i-1', 'MUL-4879', 'Counting logic looks wrong'),
          makeIssue('i-2', 'MUL-4881', 'daemon extra work dir'),
          makeIssue('i-3', 'MUL-4883', 'First PR review flow'),
        ]}
        tasksByIssueId={
          new Map([
            [
              'i-1',
              [
                makeTask({ id: 't1', agent_id: 'agent-1', issue_id: 'i-1' }),
                makeTask({ id: 't4', agent_id: 'agent-2', issue_id: 'i-1' }),
              ],
            ],
            ['i-2', [makeTask({ id: 't2', agent_id: 'agent-1', issue_id: 'i-2' })]],
            ['i-3', [makeTask({ id: 't3', agent_id: 'agent-2', issue_id: 'i-3' })]],
          ])
        }
        taskCount={4}
      />,
    );

    expect(screen.getByText('3 issues · 4 tasks')).toBeInTheDocument();
    expect(screen.getByText('MUL-4879')).toBeInTheDocument();
    expect(screen.getByText('Counting logic looks wrong')).toBeInTheDocument();
    expect(screen.getAllByTestId('actor-avatar')).toHaveLength(4);
  });

  it('says nothing about work it excludes', () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[makeIssue('i-1', 'MUL-1', 'One')]}
        tasksByIssueId={new Map([['i-1', [makeTask({ id: 't1', issue_id: 'i-1' })]]])}
        taskCount={1}
      />,
    );

    expect(screen.queryByText(/not counted/)).not.toBeInTheDocument();
    expect(screen.getByText('1 issue · 1 task')).toBeInTheDocument();
  });

  it('falls back to the agent-worded empty copy when nothing is counted', () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent issues={[]} tasksByIssueId={new Map()} taskCount={0} />,
    );

    expect(screen.getByText('No agents working right now')).toBeInTheDocument();
    expect(screen.queryByText(/not counted/)).not.toBeInTheDocument();
  });

  it('renders the Chinese copy for the counted units', () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        issues={[makeIssue('i-1', 'MUL-1', 'One')]}
        tasksByIssueId={new Map([['i-1', [makeTask({ id: 't1', issue_id: 'i-1' })]]])}
        taskCount={2}
      />,
      { locale: 'zh-Hans' },
    );

    expect(screen.getByText('1 个 issue · 2 个 task')).toBeInTheDocument();
  });
});
