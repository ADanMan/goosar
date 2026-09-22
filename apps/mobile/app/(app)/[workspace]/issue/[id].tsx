// Экран задачи: лента активности с встроенным композером комментария внизу,
// прилипающим к клавиатуре.
import { useCallback, useEffect } from 'react';
import { ActionSheetIOS, ActivityIndicator, Alert, Linking, View } from 'react-native';
import { Stack, router, useLocalSearchParams } from 'expo-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import * as Clipboard from 'expo-clipboard';
import type { Issue } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { Button } from '@/components/ui/button';
import { IconButton } from '@/components/ui/icon-button';
import { TimelineList } from '@/components/issue/timeline-list';
import { AgentHeaderBadge } from '@/components/issue/agent-header-badge';
import { InlineCommentComposer } from '@/components/issue/inline-comment-composer';
import { issueDetailOptions, issueKeys, issueTimelineOptions } from '@/data/queries/issues';
import { useDeleteIssue } from '@/data/mutations/issues';
import { pinListOptions } from '@/data/queries/pins';
import { useCreatePin, useDeletePin } from '@/data/mutations/pins';
import { useAuthStore } from '@/data/auth-store';
import { useIssueRealtime } from '@/data/realtime/use-issue-realtime';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useViewedIssuesStore } from '@/data/viewed-issues-store';
import { useCommentSelectStore } from '@/data/comment-select-store';
import { useReplyTargetStore } from '@/data/stores/reply-target-store';

export default function IssueDetail() {
  const {
    id,
    workspace: wsSlug,
    highlight,
    h,
  } = useLocalSearchParams<{
    id: string;
    workspace: string;
    highlight?: string;
    h?: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  const detail = useQuery(issueDetailOptions(wsId, id));
  const timeline = useQuery(issueTimelineOptions(wsId, id));

  useIssueRealtime(id, () => router.back());

  useEffect(() => {
    if (wsId && id) {
      useViewedIssuesStore.getState().push(wsId, id);
    }
  }, [wsId, id]);

  useEffect(() => {
    return () => {
      useCommentSelectStore.getState().clear();
      useReplyTargetStore.getState().clear();
    };
  }, []);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, id) }),
    ]);
  }, [detail, qc, wsId, id]);

  const issue = detail.data;
  const deleteIssue = useDeleteIssue();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: pins } = useQuery(pinListOptions(wsId, userId));
  const isPinned =
    !!issue && !!pins?.some((p) => p.item_type === 'issue' && p.item_id === issue.id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  const onPressMore = useCallback(() => {
    if (!issue || !wsSlug) return;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const issueLink = webUrl ? `${webUrl}/${wsSlug}/issue/${issue.identifier}` : null;
    const options: string[] = ['Cancel'];
    options.push(isPinned ? 'Unpin' : 'Pin');
    options.push('Edit details');
    if (issueLink) options.push('Copy link');
    if (issueLink) options.push('Open on web');
    options.push('Delete issue');
    const destructiveIndex = options.length - 1;
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex: 0,
        destructiveButtonIndex: destructiveIndex,
        title: issue.identifier,
      },
      (i) => {
        const label = options[i];
        if (label === 'Pin') {
          createPin.mutate({ item_type: 'issue', item_id: issue.id });
        } else if (label === 'Unpin') {
          deletePin.mutate({ itemType: 'issue', itemId: issue.id });
        } else if (label === 'Edit details') {
          if (wsSlug) router.push(`/${wsSlug}/issue/${issue.id}/edit`);
        } else if (label === 'Copy link' && issueLink) {
          Clipboard.setStringAsync(issueLink);
        } else if (label === 'Open on web' && issueLink) {
          Linking.openURL(issueLink);
        } else if (label === 'Delete issue') {
          confirmDelete(issue, () =>
            deleteIssue.mutate(issue.id, {
              onSuccess: () => router.back(),
            }),
          );
        }
      },
    );
  }, [issue, wsSlug, deleteIssue, isPinned, createPin, deletePin]);

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          title: issue?.identifier ?? 'Issue',
          headerBackTitle: 'Back',
          headerRight: issue
            ? () => (
                <View className="flex-row items-center gap-2">
                  {/* Ambient agent-working badge — renders null when no
                   *  active tasks, so it doesn't crowd the header in the
                   *  common case. See agent-header-badge.tsx. */}
                  <AgentHeaderBadge issueId={id} />
                  <IconButton
                    name="ellipsis-horizontal"
                    onPress={onPressMore}
                    accessibilityLabel="Issue actions"
                  />
                </View>
              )
            : undefined,
        }}
      />
      {detail.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : detail.error || !issue ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
            Failed to load issue:{' '}
            {detail.error instanceof Error ? detail.error.message : 'not found'}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <View className="flex-1">
          <TimelineList
            issue={issue}
            entries={timeline.data}
            timelineLoading={timeline.isLoading}
            refreshing={detail.isRefetching || timeline.isRefetching}
            onRefresh={onRefresh}
            highlightCommentId={highlight}
            highlightNonce={h}
          />
          <InlineCommentComposer issueId={id} />
        </View>
      )}
    </View>
  );
}

function confirmDelete(issue: Issue, onConfirm: () => void) {
  Alert.alert(
    'Delete issue?',
    `${issue.identifier} and its comments, reactions, and attachments will be permanently deleted. This cannot be undone.`,
    [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Delete', style: 'destructive', onPress: onConfirm },
    ],
  );
}
