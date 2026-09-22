'use client';

import { useQuery } from '@tanstack/react-query';
import { api } from '@goosar/core/api';

export function useAttachmentHtmlText(attachmentId: string | null | undefined) {
  return useQuery({
    queryKey: ['attachment-content', attachmentId ?? ''] as const,
    queryFn: () => api.getAttachmentTextContent(attachmentId as string),
    enabled: !!attachmentId,
    retry: false,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
  });
}
