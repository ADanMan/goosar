'use client';

import { useMemo } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { api } from '@goosar/core/api';
import { issueKeys } from '@goosar/core/issues/queries';
import type { IssueAssigneeType, IssueStatus, IssueTriggerPreviewItem } from '@goosar/core/types';

export interface UseIssueTriggerPreviewParams {
  issueIds?: string[];
  isCreate?: boolean;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  status?: IssueStatus;
  enabled?: boolean;
}

export interface UseIssueTriggerPreviewResult {
  triggers: IssueTriggerPreviewItem[];
  totalCount: number;
  isLoading: boolean;
  handoffSupported: boolean;
}

const EMPTY: IssueTriggerPreviewItem[] = [];

function previewSignature(params: UseIssueTriggerPreviewParams): string {
  return JSON.stringify({
    ids: [...(params.issueIds ?? [])].sort(),
    create: params.isCreate ?? false,
    at: params.assigneeType ?? null,
    aid: params.assigneeId ?? null,
    status: params.status ?? null,
  });
}

export function useIssueTriggerPreview(
  params: UseIssueTriggerPreviewParams,
): UseIssueTriggerPreviewResult {
  const hasTarget =
    (!!params.assigneeType && !!params.assigneeId) || !!params.status || (params.isCreate ?? false);
  const enabled = (params.enabled ?? true) && hasTarget;

  const signature = useMemo(() => previewSignature(params), [params]);

  const previewQuery = useQuery({
    queryKey: issueKeys.issueTriggerPreview(signature),
    queryFn: () =>
      api.previewIssueTrigger({
        issueIds: params.issueIds,
        isCreate: params.isCreate,
        assigneeType: params.assigneeType,
        assigneeId: params.assigneeId,
        status: params.status,
      }),
    enabled,
    retry: false,
    staleTime: 0,
    placeholderData: keepPreviousData,
  });

  const triggers = previewQuery.data?.triggers ?? EMPTY;
  return {
    triggers,
    totalCount: previewQuery.data?.total_count ?? 0,
    isLoading: enabled && previewQuery.isLoading,
    handoffSupported: triggers.length > 0 && triggers.every((t) => t.handoff_supported === true),
  };
}
