'use client';

import { useMemo, type RefObject } from 'react';
import type { DraftUpload } from '@goosar/core/drafts';
import type { UploadContext } from '@goosar/core/hooks/use-file-upload';
import { useCommentDraftStore, type CommentDraftKey } from '@goosar/core/issues/stores';
import type { UploadGate } from '../../editor/use-upload-gate';
import type { ContentEditorRef } from '../../editor/content-editor';
import {
  useCoordinatedUploads,
  type CoordinatedUploads,
  type UploadDraftBinding,
} from '../../editor/use-coordinated-uploads';

const EMPTY_UPLOADS: DraftUpload[] = [];

export type CommentUploads = CoordinatedUploads;

export function useCommentUploads(
  draftKey: CommentDraftKey | undefined,
  ctx: UploadContext,
  editorGate: UploadGate,
  editorRef: RefObject<ContentEditorRef | null>,
): CommentUploads {
  const storeUploads = useCommentDraftStore((s) =>
    draftKey ? s.getUploads(draftKey) : EMPTY_UPLOADS,
  );

  const binding = useMemo<UploadDraftBinding | undefined>(() => {
    if (!draftKey) return undefined;
    return {
      registryKey: `comment:${draftKey}`,
      getUploads: () => useCommentDraftStore.getState().getUploads(draftKey),
      addUpload: (u) => useCommentDraftStore.getState().addUpload(draftKey, u),
      settleUpload: (id, att) => useCommentDraftStore.getState().settleUpload(draftKey, id, att),
      failUpload: (id, err) => useCommentDraftStore.getState().failUpload(draftKey, id, err),
      removeUpload: (id) => useCommentDraftStore.getState().removeUpload(draftKey, id),
      getBody: () => useCommentDraftStore.getState().getDraft(draftKey) ?? '',
      appendToBody: (md) => useCommentDraftStore.getState().appendToDraftContent(draftKey, md),
    };
  }, [draftKey]);

  return useCoordinatedUploads(binding, storeUploads, ctx, editorGate, editorRef);
}
