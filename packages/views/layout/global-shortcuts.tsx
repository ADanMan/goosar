'use client';

import { useEffect } from 'react';
import { useSidebar } from '@goosar/ui/components/ui/sidebar';
import {
  getShortcut,
  isEditableShortcutTarget,
  shortcutMatchesEvent,
  SHORTCUT_ACTION_BY_ID,
  useShortcutStore,
  type ShortcutActionId,
} from '@goosar/core/shortcuts';
import { openCreateIssueWithPreference } from '@goosar/core/issues/stores';
import { useModalStore } from '@goosar/core/modals';
import { useWorkspacePaths } from '@goosar/core/paths';
import { isImeComposing } from '@goosar/core/utils';
import { useNavigation } from '../navigation';
import { useSearchStore } from '../search/search-store';

const GLOBAL_ACTIONS: readonly ShortcutActionId[] = [
  'openSearch',
  'createIssue',
  'toggleSidebar',
  'goInbox',
  'goChat',
  'goMyIssues',
  'goIssues',
  'goProjects',
  'goAutopilots',
  'goAgents',
  'goSquads',
  'goUsage',
  'goRuntimes',
  'goSkills',
  'goSettings',
];

export function shouldIgnoreGlobalShortcutEvent(event: KeyboardEvent): boolean {
  return event.defaultPrevented || event.repeat || isImeComposing(event);
}

export function GlobalShortcuts() {
  const { toggleSidebar } = useSidebar();
  const navigation = useNavigation();
  const workspacePaths = useWorkspacePaths();

  const overrides = useShortcutStore((state) => state.overrides);

  useEffect(() => {
    const destinations: Partial<Record<ShortcutActionId, string>> = {
      goInbox: workspacePaths.inbox(),
      goChat: workspacePaths.chat(),
      goMyIssues: workspacePaths.myIssues(),
      goIssues: workspacePaths.issues(),
      goProjects: workspacePaths.projects(),
      goAutopilots: workspacePaths.autopilots(),
      goAgents: workspacePaths.agents(),
      goSquads: workspacePaths.squads(),
      goUsage: workspacePaths.usage(),
      goRuntimes: workspacePaths.runtimes(),
      goSkills: workspacePaths.skills(),
      goSettings: workspacePaths.settings(),
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (shouldIgnoreGlobalShortcutEvent(event)) return;

      const actionId = GLOBAL_ACTIONS.find((candidate) => {
        const action = SHORTCUT_ACTION_BY_ID[candidate];
        if (!action.allowInEditable && isEditableShortcutTarget(event.target)) {
          return false;
        }
        return shortcutMatchesEvent(getShortcut(candidate), event);
      });
      if (!actionId) return;

      event.preventDefault();
      if (actionId === 'openSearch') {
        useSearchStore.getState().toggle();
        return;
      }
      if (actionId === 'toggleSidebar') {
        toggleSidebar();
        return;
      }
      if (actionId === 'createIssue') {
        if (useModalStore.getState().modal) return;
        const projectMatch = navigation.pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
        const data = projectMatch ? { project_id: projectMatch[1] } : undefined;
        openCreateIssueWithPreference(data);
        return;
      }

      const destination = destinations[actionId];
      if (destination && destination !== navigation.pathname) {
        navigation.push(destination);
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [navigation, overrides, toggleSidebar, workspacePaths]);

  return null;
}
