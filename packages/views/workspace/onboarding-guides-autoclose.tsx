'use client';

import { useCallback, useEffect, useRef } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { api } from '@goosar/core/api';
import { useAuthStore } from '@goosar/core/auth';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { useWSEvent } from '@goosar/core/realtime';
import { issueKeys } from '@goosar/core/issues/queries';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import type { Issue, IssueStatus } from '@goosar/core/types';
import { prepareWorkspaceHelper } from './helper-setup';
import {
  CREATE_AGENT_GUIDE_ISSUE_TITLE,
  INSTALL_RUNTIME_ISSUE_TITLE,
  ONBOARDING_SEED,
  ONBOARDING_SEED_METADATA_KEY,
} from '../onboarding/templates';

export function OnboardingGuidesAutoClose() {
  const me = useAuthStore((s) => s.user);
  const wsId = useCurrentWorkspace()?.id ?? null;
  const qc = useQueryClient();

  const recheckTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (recheckTimerRef.current !== null) {
        clearTimeout(recheckTimerRef.current);
        recheckTimerRef.current = null;
      }
    },
    [],
  );

  const handleDaemonRegister = useCallback(() => {
    if (!me || !me.onboarded_at || !wsId) return;
    const params: AutoCloseParams = { workspaceId: wsId, userId: me.id, qc };
    void maybeCloseOnboardingGuides(params).then((outcome) => {
      if (outcome !== 'guide_missing' && outcome !== 'no_agent') return;
      if (recheckTimerRef.current !== null) return;
      recheckTimerRef.current = setTimeout(() => {
        recheckTimerRef.current = null;
        void maybeCloseOnboardingGuides(params);
      }, GUIDE_SEED_RECHECK_MS);
    });
  }, [me, wsId, qc]);
  useWSEvent('daemon:register', handleDaemonRegister);

  return null;
}

OnboardingGuidesAutoClose.displayName = 'OnboardingGuidesAutoClose';

const OPEN_GUIDE_STATUSES: IssueStatus[] = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'blocked',
];

const GUIDE_PROBE_LIMIT = 100;

const GUIDE_SEED_RECHECK_MS = 12_000;

type AutoCloseOutcome =
  'guides_closed' | 'no_agent' | 'guide_missing' | 'already_settled' | 'failed';

const pendingAutoClose = new Map<string, Promise<AutoCloseOutcome>>();
const settledAutoClose = new Set<string>();

interface AutoCloseParams {
  workspaceId: string;
  userId: string;
  qc: QueryClient;
}

function maybeCloseOnboardingGuides(params: AutoCloseParams): Promise<AutoCloseOutcome> {
  const { workspaceId } = params;
  if (settledAutoClose.has(workspaceId)) {
    return Promise.resolve('already_settled');
  }
  const pending = pendingAutoClose.get(workspaceId);
  if (pending) return pending;
  const run = runAutoClose(params)
    // Background nicety: a failure must never surface UI from the
    // workspace shell. The next daemon:register re-runs the gates.
    .catch((): AutoCloseOutcome => 'failed')
    .finally(() => {
      pendingAutoClose.delete(workspaceId);
    });
  pendingAutoClose.set(workspaceId, run);
  return run;
}

async function runAutoClose({
  workspaceId,
  userId,
  qc,
}: AutoCloseParams): Promise<AutoCloseOutcome> {
  const helper = await prepareWorkspaceHelper(workspaceId, null);
  if (!helper) return 'no_agent';

  const { agentGuide, installGuide } = await findOpenGuides(workspaceId, userId);
  if (!agentGuide) return 'guide_missing';

  settledAutoClose.add(workspaceId);

  await Promise.all(
    [agentGuide, installGuide]
      .filter((issue): issue is Issue => issue != null)
      .map(async (issue) => {
        try {
          await api.updateIssue(issue.id, { status: 'done' });
        } catch {
          // Best-effort; a still-open guide is retried on the next event.
        }
      }),
  );

  qc.invalidateQueries({ queryKey: issueKeys.all(workspaceId) });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(workspaceId) });
  return 'guides_closed';
}

async function findOpenGuides(
  workspaceId: string,
  userId: string,
): Promise<{ agentGuide: Issue | null; installGuide: Issue | null }> {
  const probeBase = {
    workspace_id: workspaceId,
    assignee_id: userId,
    statuses: OPEN_GUIDE_STATUSES,
  };
  const stampedRes = await api.listIssues({
    ...probeBase,
    metadata: {
      [ONBOARDING_SEED_METADATA_KEY]: ONBOARDING_SEED.createAgentGuide,
    },
    limit: 1,
  });
  const stampedGuide = stampedRes?.issues?.[0] ?? null;
  if (stampedGuide) {
    const installRes = await api.listIssues({
      ...probeBase,
      metadata: {
        [ONBOARDING_SEED_METADATA_KEY]: ONBOARDING_SEED.installRuntime,
      },
      limit: 1,
    });
    return {
      agentGuide: stampedGuide,
      installGuide: installRes?.issues?.[0] ?? null,
    };
  }

  const legacyRes = await api.listIssues({
    ...probeBase,
    limit: GUIDE_PROBE_LIMIT,
  });
  const issues: Issue[] = legacyRes?.issues ?? [];
  const guideTitles = new Set<string>(Object.values(CREATE_AGENT_GUIDE_ISSUE_TITLE));
  const installTitles = new Set<string>(Object.values(INSTALL_RUNTIME_ISSUE_TITLE));
  return {
    agentGuide: issues.find((i) => guideTitles.has(i.title)) ?? null,
    installGuide: issues.find((i) => installTitles.has(i.title)) ?? null,
  };
}
