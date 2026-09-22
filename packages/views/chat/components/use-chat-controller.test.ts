import { describe, it, expect } from 'vitest';
import { QueryClient, type InfiniteData } from '@tanstack/react-query';
import { chatKeys } from '@goosar/core/chat/queries';
import type { ChatMessage, ChatMessagesPage, ChatPendingTask } from '@goosar/core/types';
import {
  hasInFlightPendingTask,
  isStillOnComposeTarget,
  planProjectContextChange,
} from './use-chat-controller';

const sid = 'session-1';

function msg(id: string): ChatMessage {
  return {
    id,
    chat_session_id: sid,
    role: 'user',
    content: 'hi',
    task_id: null,
    created_at: '2026-07-08T00:00:00Z',
  };
}

describe('hasInFlightPendingTask', () => {
  it('is false with an empty cache', () => {
    expect(hasInFlightPendingTask(new QueryClient(), sid)).toBe(false);
  });

  it('is false when only real cached history exists (no in-flight task)', () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(sid), [msg('real-1'), msg('real-2')]);
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sid), {
      pages: [{ messages: [msg('real-1')], limit: 50, has_more: false, next_cursor: null }],
      pageParams: [null],
    });
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sid), {} as ChatPendingTask);
    expect(hasInFlightPendingTask(qc, sid)).toBe(false);
  });

  it('is true while a pending task is in flight (task_id set)', () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sid), {
      task_id: '11111111-1111-4111-8111-111111111111',
      status: 'queued',
      created_at: '2026-07-08T00:00:00Z',
    });
    expect(hasInFlightPendingTask(qc, sid)).toBe(true);
  });
});

describe('isStillOnComposeTarget', () => {
  it('is true when the user never left the session they sent from', () => {
    expect(isStillOnComposeTarget(sid, sid)).toBe(true);
  });

  it('is true for a new chat the user is still sitting in', () => {
    expect(isStillOnComposeTarget(null, null)).toBe(true);
  });

  it('is false once the user opens a different session mid-send', () => {
    expect(isStillOnComposeTarget('session-2', sid)).toBe(false);
  });

  it('is false when the user starts a new chat mid-send from a session', () => {
    expect(isStillOnComposeTarget(null, sid)).toBe(false);
  });
});

describe('planProjectContextChange', () => {
  const sessionB = { id: 'sB', agent_id: 'agent-b' };

  it('waits when an active session id is set but its row has not loaded yet', () => {
    expect(
      planProjectContextChange({
        targetProjectId: 'project-x',
        activeSessionId: 'sB',
        currentSession: null,
      }),
    ).toEqual({ kind: 'awaitSession' });
  });

  it("detaches in place when the current session's project is removed", () => {
    expect(
      planProjectContextChange({
        targetProjectId: null,
        activeSessionId: 'sB',
        currentSession: sessionB,
      }),
    ).toEqual({ kind: 'detachCurrent', sessionId: 'sB' });
  });

  it("starts a fresh chat pinned to the open session's agent, ignoring a stale selectedAgentId", () => {
    expect(
      planProjectContextChange({
        targetProjectId: 'project-x',
        activeSessionId: 'sB',
        currentSession: sessionB,
      }),
    ).toEqual({ kind: 'startFreshChat', agentId: 'agent-b', projectId: 'project-x' });
  });

  it('only adjusts the new-chat draft project when there is no open session', () => {
    expect(
      planProjectContextChange({
        targetProjectId: 'project-x',
        activeSessionId: null,
        currentSession: null,
      }),
    ).toEqual({ kind: 'setDraftProject', projectId: 'project-x' });
  });
});
