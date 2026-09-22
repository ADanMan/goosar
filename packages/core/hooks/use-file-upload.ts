'use client';

import { useState, useCallback } from 'react';
import type { ApiClient } from '../api/client';
import type { Attachment } from '../types';
import { attachmentDownloadPath } from '../types/attachment-url';
import { MAX_FILE_SIZE } from '../constants/upload';

export type UploadResult = Attachment & {
  link: string;
  markdownLink: string;
};

export interface UploadContext {
  issueId?: string;
  commentId?: string;
  chatSessionId?: string;
}

function pickMarkdownLink(att: Attachment): string {
  if (att.markdown_url) return att.markdown_url;
  if (att.id) return attachmentDownloadPath(att.id);
  return att.url;
}

export function toUploadResult(att: Attachment): UploadResult {
  return { ...att, link: att.url, markdownLink: pickMarkdownLink(att) };
}

export function useFileUpload(
  api: ApiClient,
  onError?: (error: Error, file: File) => void,
) {
  const [inFlight, setInFlight] = useState(0);
  const uploading = inFlight > 0;

  const upload = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      if (file.size > MAX_FILE_SIZE) {
        throw new Error('File exceeds 100 MB limit');
      }

      setInFlight((n) => n + 1);
      try {
        const att: Attachment = await api.uploadFile(file, {
          issueId: ctx?.issueId,
          commentId: ctx?.commentId,
          chatSessionId: ctx?.chatSessionId,
        });
        return toUploadResult(att);
      } finally {
        setInFlight((n) => n - 1);
      }
    },
    [api],
  );

  const uploadWithToast = useCallback(
    async (file: File, ctx?: UploadContext): Promise<UploadResult | null> => {
      try {
        return await upload(file, ctx);
      } catch (err) {
        onError?.(err instanceof Error ? err : new Error('Upload failed'), file);
        return null;
      }
    },
    [upload, onError],
  );

  return { upload, uploadWithToast, uploading };
}
