'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  parseTabSubject,
  resolveTabPresentation,
  useCurrentWorkspace,
  type TabSubject,
  type TabVisual,
  type TabTitleSpec,
  type TabEntityData,
  type TabLabelKey,
} from '@goosar/core/paths';
import { issueDetailOptions } from '@goosar/core/issues/queries';
import { projectDetailOptions } from '@goosar/core/projects/queries';
import { autopilotDetailOptions } from '@goosar/core/autopilots/queries';
import {
  skillDetailOptions,
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from '@goosar/core/workspace/queries';
import { runtimeListOptions } from '@goosar/core/runtimes/queries';
import { runtimeDisplayName } from '@goosar/core/runtimes';
import { chatSessionsOptions } from '@goosar/core/chat/queries';
import { inboxListOptions, archivedInboxListOptions } from '@goosar/core/inbox/queries';
import { cn } from '@goosar/ui/lib/utils';
import { StatusIcon } from '../issues/components';
import { ProjectIcon } from '../projects/components/project-icon';
import { ActorAvatar } from '../common/actor-avatar';
import { getInboxDisplayTitle } from '../inbox/components/inbox-display';
import { useT } from '../i18n';
import { ROUTE_ICON_COMPONENTS } from './route-icon-components';

const NONE = '__tab_presentation_none__';

const PENDING_RESOURCE_KEYS: ReadonlySet<TabLabelKey> = new Set<TabLabelKey>([
  'issue',
  'project',
  'autopilot',
  'agent',
  'member',
  'squad',
  'skill',
  'machine',
  'runtime',
]);

function useTabEntityData(subject: TabSubject, wsId: string): TabEntityData {
  const { t: chatT } = useT('chat');

  const inboxList = useQuery({ ...inboxListOptions(wsId), enabled: false }).data;
  const archivedInboxList = useQuery({
    ...archivedInboxListOptions(wsId),
    enabled: false,
  }).data;
  const activeInboxList =
    subject.kind === 'inbox' && subject.archived ? archivedInboxList : inboxList;
  const inboxItem =
    subject.kind === 'inbox' && subject.selectedKey
      ? (activeInboxList?.find((i) => (i.issue_id ?? i.id) === subject.selectedKey) ?? null)
      : null;

  const issueId = subject.kind === 'issue' ? subject.id : (inboxItem?.issue_id ?? '');
  const issue = useQuery({
    ...issueDetailOptions(wsId, issueId || NONE),
    enabled: false,
  }).data;

  const project = useQuery({
    ...projectDetailOptions(wsId, subject.kind === 'project' ? subject.id : NONE),
    enabled: false,
  }).data;
  const autopilot = useQuery({
    ...autopilotDetailOptions(wsId, subject.kind === 'autopilot' ? subject.id : NONE),
    enabled: false,
  }).data;
  const skill = useQuery({
    ...skillDetailOptions(wsId, subject.kind === 'skill' ? subject.id : NONE),
    enabled: false,
  }).data;

  const agents = useQuery({ ...agentListOptions(wsId), enabled: false }).data;
  const members = useQuery({ ...memberListOptions(wsId), enabled: false }).data;
  const squads = useQuery({ ...squadListOptions(wsId), enabled: false }).data;
  const runtimes = useQuery({ ...runtimeListOptions(wsId), enabled: false }).data;
  const sessions = useQuery({ ...chatSessionsOptions(wsId), enabled: false }).data;

  const data: TabEntityData = {};
  switch (subject.kind) {
    case 'issue':
      if (issue) {
        data.issue = {
          identifier: issue.identifier,
          title: issue.title,
          status: issue.status,
        };
      }
      break;
    case 'project':
      if (project) data.project = { icon: project.icon, title: project.title };
      break;
    case 'autopilot':
      if (autopilot) data.autopilot = { title: autopilot.autopilot.title };
      break;
    case 'skill':
      if (skill) data.skill = { name: skill.name };
      break;
    case 'actor': {
      const name =
        subject.actorType === 'agent'
          ? agents?.find((a) => a.id === subject.id)?.name
          : subject.actorType === 'member'
            ? members?.find((m) => m.user_id === subject.id)?.name
            : squads?.find((s) => s.id === subject.id)?.name;
      if (name) data.actorName = name;
      break;
    }
    case 'machine': {
      const rt = runtimes?.find((r) => r.id === subject.machineId);
      if (rt) data.machine = { name: runtimeDisplayName(rt) };
      break;
    }
    case 'runtime': {
      const rt = runtimes?.find((r) => r.id === subject.runtimeId);
      if (rt) data.runtime = { name: runtimeDisplayName(rt) };
      break;
    }
    case 'chat':
      if (subject.sessionId) {
        const s = sessions?.find((x) => x.id === subject.sessionId);
        if (s) data.chatSessionTitle = s.title?.trim() || chatT(($) => $.window.untitled);
      }
      break;
    case 'inbox':
      if (inboxItem) {
        if (inboxItem.issue_id && issue) {
          data.inboxSelection = {
            kind: 'issue',
            identifier: issue.identifier,
            title: issue.title,
          };
        } else if (!inboxItem.issue_id) {
          data.inboxSelection = {
            kind: 'item',
            title: getInboxDisplayTitle(inboxItem),
          };
        }
      }
      break;
  }
  return data;
}

function useTabTitle(spec: TabTitleSpec, fallbackTitle?: string): string {
  const { t: layoutT } = useT('layout');
  switch (spec.kind) {
    case 'text':
      return spec.text;
    case 'nav':
      return layoutT(($) => $.nav[spec.navKey]);
    case 'tab': {
      if (PENDING_RESOURCE_KEYS.has(spec.tabKey)) {
        const clean = fallbackTitle?.trim();
        if (clean) return clean;
      }
      return layoutT(($) => $.tab[spec.tabKey]);
    }
  }
}

export interface TabPresentationResult {
  visual: TabVisual;
  title: string;
}

export function useTabPresentation(url: string, fallbackTitle?: string): TabPresentationResult {
  const subject = useMemo(() => parseTabSubject(url), [url]);
  const ws = useCurrentWorkspace();
  const wsId = ws?.id ?? '';
  const data = useTabEntityData(subject, wsId);
  const { visual, title: titleSpec } = resolveTabPresentation(subject, data);
  const title = useTabTitle(titleSpec, fallbackTitle);

  const safeVisual: TabVisual =
    visual.kind === 'actor' && !wsId
      ? {
          kind: 'icon',
          icon:
            visual.actorType === 'squad'
              ? 'Users'
              : visual.actorType === 'member'
                ? 'CircleUser'
                : 'Bot',
        }
      : visual;

  return { visual: safeVisual, title };
}

export function ResourceLeadingVisual({
  visual,
  className,
}: {
  visual: TabVisual;
  className?: string;
}) {
  let inner: React.ReactNode;
  switch (visual.kind) {
    case 'icon': {
      const Icon = ROUTE_ICON_COMPONENTS[visual.icon];
      inner = <Icon className="size-3.5" />;
      break;
    }
    case 'issue-status':
      inner = <StatusIcon status={visual.status ?? ''} className="size-3.5" />;
      break;
    case 'project-icon':
      inner = <ProjectIcon project={{ icon: visual.icon }} size="sm" />;
      break;
    case 'actor':
      inner = (
        <ActorAvatar
          actorType={visual.actorType}
          actorId={visual.id}
          size="xs"
          profileLink={false}
        />
      );
      break;
  }
  return (
    <span className={cn('flex size-4 shrink-0 items-center justify-center', className)}>
      {inner}
    </span>
  );
}
