'use client';

import { useCallback, useState, type RefObject } from 'react';
import type { ContentEditorRef } from './content-editor';

interface UploadGate {
  uploading: boolean;
  onUploadingChange: (uploading: boolean) => void;
  isBlocked: () => boolean;
}

function useUploadGate(editorRef: RefObject<ContentEditorRef | null>): UploadGate {
  const [uploading, setUploading] = useState(false);
  const isBlocked = useCallback(() => editorRef.current?.hasActiveUploads() === true, [editorRef]);
  return { uploading, onUploadingChange: setUploading, isBlocked };
}

export { useUploadGate, type UploadGate };
