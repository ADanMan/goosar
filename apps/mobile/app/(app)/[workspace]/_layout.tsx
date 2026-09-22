import { useEffect } from 'react';
import type { ComponentProps } from 'react';
import { Redirect, Stack, useLocalSearchParams } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { workspaceListOptions } from '@/data/queries/workspaces';
import { useWorkspaceStore } from '@/data/workspace-store';
import { RealtimeProvider } from '@/data/realtime/realtime-provider';
import { useInboxRealtime } from '@/data/realtime/use-inbox-realtime';
import { useIssuesRealtime } from '@/data/realtime/use-issues-realtime';
import { useMyIssuesRealtime } from '@/data/realtime/use-my-issues-realtime';
import { useChatSessionsRealtime } from '@/data/realtime/use-chat-sessions-realtime';
import { useProjectsRealtime } from '@/data/realtime/use-projects-realtime';
import { usePinsRealtime } from '@/data/realtime/use-pins-realtime';
import { usePresenceRealtime } from '@/data/realtime/use-presence-realtime';
import { useWorkspacePresencePrefetch } from '@/lib/use-workspace-presence-prefetch';
import { ModalCloseButton } from '@/components/ui/modal-close-button';
import { useNewIssueDraftResetOnWorkspaceChange } from '@/data/stores/new-issue-draft-store';
import { useNewProjectDraftResetOnWorkspaceChange } from '@/data/stores/new-project-draft-store';
import { useChatSessionPickerResetOnWorkspaceChange } from '@/data/stores/chat-session-picker-store';

const SHEET_OPTIONS: ComponentProps<typeof Stack.Screen>['options'] = {
  presentation: 'formSheet',
  sheetGrabberVisible: true,
  sheetAllowedDetents: [0.6, 0.95],
  sheetCornerRadius: 20,
  contentStyle: { flex: 1 },
  headerShown: false,
};

export const unstable_settings = { anchor: '(tabs)' } as const;

function RealtimeSubscriptions() {
  useInboxRealtime();
  useIssuesRealtime();
  useMyIssuesRealtime();
  useChatSessionsRealtime();
  useProjectsRealtime();
  usePinsRealtime();
  useWorkspacePresencePrefetch();
  usePresenceRealtime();
  return null;
}

export default function WorkspaceLayout() {
  const { workspace: slug } = useLocalSearchParams<{ workspace: string }>();
  const { data: workspaces, isLoading } = useQuery(workspaceListOptions());
  const setCurrentWorkspace = useWorkspaceStore((s) => s.setCurrentWorkspace);

  const matched = workspaces?.find((w) => w.slug === slug);

  useEffect(() => {
    if (matched) {
      setCurrentWorkspace(matched.id, matched.slug);
    }
  }, [matched, setCurrentWorkspace]);

  useNewIssueDraftResetOnWorkspaceChange(matched?.id ?? null);
  useNewProjectDraftResetOnWorkspaceChange(matched?.id ?? null);
  useChatSessionPickerResetOnWorkspaceChange(matched?.id ?? null);

  if (isLoading) return null;

  if (!matched) return <Redirect href="/select-workspace" />;

  return (
    <RealtimeProvider>
      <RealtimeSubscriptions />
      <Stack>
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen
          name="issue/[id]"
          options={{
            title: 'Issue',
            headerBackTitle: 'Back',
          }}
        />
        <Stack.Screen
          name="project/[id]"
          options={{
            title: 'Project',
            headerBackTitle: 'Back',
          }}
        />
        <Stack.Screen
          name="project/[id]/edit"
          options={{
            title: 'Edit Project',
            presentation: 'modal',
            headerLeft: () => <ModalCloseButton />,
          }}
        />
        <Stack.Screen
          name="issue/[id]/edit"
          options={{
            title: 'Edit Issue',
            presentation: 'modal',
            headerLeft: () => <ModalCloseButton />,
          }}
        />
        <Stack.Screen
          name="project/new"
          options={{
            title: 'New Project',
            presentation: 'modal',
            headerLeft: () => <ModalCloseButton />,
          }}
        />
        {/* Issue-detail formSheet pickers. All share the same sheet config:
            explicit numeric detents to dodge expo/expo#42904+#42965 (the
            `fitToContents` zero-size / padding bugs on iOS 26 + Expo 55),
            iOS native grabber, and contentStyle.height=100% as a safety
            net against the same zero-size class of bugs. */}
        <Stack.Screen name="issue/[id]/picker/status" options={SHEET_OPTIONS} />
        <Stack.Screen name="issue/[id]/picker/priority" options={SHEET_OPTIONS} />
        {/* Experiment: assignee uses iOS-native nav header + UISearchController
            instead of the body-rendered header pattern in SHEET_OPTIONS.
            Eliminates the #3634 overlap class of bugs and the focus-loss
            footgun of a custom TextInput inside ListHeaderComponent. The
            route file wires `headerSearchBarOptions` via setOptions. If this
            proves out, propagate to label / project / other search pickers
            and update CLAUDE.md Lesson 6 with a carve-out. */}
        <Stack.Screen
          name="issue/[id]/picker/assignee"
          options={{
            ...SHEET_OPTIONS,
            headerShown: true,
            title: 'Assignee',
          }}
        />
        <Stack.Screen name="issue/[id]/picker/label" options={SHEET_OPTIONS} />
        <Stack.Screen
          name="mention-picker"
          options={{
            ...SHEET_OPTIONS,
            headerShown: true,
            title: 'Mention',
          }}
        />
        <Stack.Screen name="issue/[id]/picker/project" options={SHEET_OPTIONS} />
        <Stack.Screen name="issue/[id]/picker/due-date" options={SHEET_OPTIONS} />
        <Stack.Screen name="issue/[id]/runs" options={SHEET_OPTIONS} />
        {/* Full emoji picker for a comment reaction. Pushed from the "+"
            button inside the comment long-press tapback row — see
            components/issue/comment-context-menu.tsx. */}
        <Stack.Screen name="issue/[id]/comment/[commentId]/emoji-picker" options={SHEET_OPTIONS} />
        {/* Project-detail formSheet pickers. */}
        <Stack.Screen name="project/[id]/picker/status" options={SHEET_OPTIONS} />
        <Stack.Screen name="project/[id]/picker/priority" options={SHEET_OPTIONS} />
        <Stack.Screen name="project/[id]/picker/lead" options={SHEET_OPTIONS} />
        <Stack.Screen name="project/[id]/add-resource" options={SHEET_OPTIONS} />
        {/* New-issue draft formSheet pickers — stacked on top of the
            new-issue.tsx Stack.Screen (which is itself a `modal`).
            Expo Router 55 / RN Screens 4 support a formSheet pushed on top
            of a modal in the same Stack. */}
        <Stack.Screen name="new-issue-picker/status" options={SHEET_OPTIONS} />
        <Stack.Screen name="new-issue-picker/priority" options={SHEET_OPTIONS} />
        <Stack.Screen
          name="new-issue-picker/assignee"
          options={{
            ...SHEET_OPTIONS,
            headerShown: true,
            title: 'Assignee',
          }}
        />
        <Stack.Screen name="new-issue-picker/project" options={SHEET_OPTIONS} />
        <Stack.Screen name="new-issue-picker/due-date" options={SHEET_OPTIONS} />
        {/* New-project draft formSheet pickers — same pattern as
            new-issue-picker/*. Stacked on top of `project/new` (a modal). */}
        <Stack.Screen name="new-project-picker/status" options={SHEET_OPTIONS} />
        <Stack.Screen name="new-project-picker/priority" options={SHEET_OPTIONS} />
        {/* Shared filter sheet for My Issues and the workspace Issues page —
            chooses the right view-store via `?scope=my|all` URL param. */}
        <Stack.Screen name="issues-filter" options={SHEET_OPTIONS} />
        {/* Chat session-switch sheet. */}
        <Stack.Screen name="chat-sessions" options={SHEET_OPTIONS} />
        {/* Workspace switcher — reached from the More popover's collapsed
            WorkspaceCard. Two-step (pick → iOS Alert confirm → switch). */}
        <Stack.Screen name="switch-workspace" options={SHEET_OPTIONS} />
        <Stack.Screen name="more/issues" options={{ title: 'Issues', headerBackTitle: 'Back' }} />
        <Stack.Screen
          name="more/projects"
          options={{ title: 'Projects', headerBackTitle: 'Back' }}
        />
        <Stack.Screen name="more/agents" options={{ title: 'Agents', headerBackTitle: 'Back' }} />
        <Stack.Screen name="more/pins" options={{ title: 'Pinned', headerBackTitle: 'Back' }} />
        <Stack.Screen
          name="more/settings"
          options={{ title: 'Settings', headerBackTitle: 'Back' }}
        />
        <Stack.Screen
          name="more/settings/profile"
          options={{ title: 'Profile', headerBackTitle: 'Settings' }}
        />
        <Stack.Screen
          name="more/settings/notifications"
          options={{ title: 'Notifications', headerBackTitle: 'Settings' }}
        />
        <Stack.Screen
          name="new-issue"
          options={{
            title: 'New Issue',
            presentation: 'modal',
            headerLeft: () => <ModalCloseButton />,
          }}
        />
        <Stack.Screen
          name="search"
          options={{
            title: 'Search',
            presentation: 'modal',
            headerLeft: () => <ModalCloseButton />,
          }}
        />
      </Stack>
    </RealtimeProvider>
  );
}
