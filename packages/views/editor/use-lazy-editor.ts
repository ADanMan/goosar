'use client';

import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';
import type { TextAnchor } from './text-anchor';

export interface LazyEditorHandle {
  focus: () => void;
  focusAtCoords?: (coords: { x: number; y: number }) => void;
  focusAtAnchor?: (anchor: TextAnchor) => void;
  uploadFile?: (file: File) => void;
}

export interface LazyFocusTarget {
  x: number;
  y: number;
  anchor?: TextAnchor;
}

export interface UseLazyEditorOptions {
  initialActive?: boolean;
  editorRef: RefObject<LazyEditorHandle | null>;
  resetKey?: unknown;
}

export function useLazyEditor({
  initialActive = false,
  editorRef,
  resetKey,
}: UseLazyEditorOptions) {
  const [active, setActive] = useState(initialActive);
  const [ready, setReady] = useState(false);
  const focusTargetRef = useRef<LazyFocusTarget | null>(null);
  const focusPendingRef = useRef(false);
  const pendingFilesRef = useRef<File[]>([]);

  const [prevResetKey, setPrevResetKey] = useState(resetKey);
  if (resetKey !== prevResetKey) {
    setPrevResetKey(resetKey);
    setActive(initialActive);
    setReady(false);
    focusTargetRef.current = null;
    focusPendingRef.current = false;
    pendingFilesRef.current = [];
  }

  const focusAtTarget = useCallback(
    (target: LazyFocusTarget | null) => {
      const handle = editorRef.current;
      if (!handle) return;
      if (target?.anchor && handle.focusAtAnchor) handle.focusAtAnchor(target.anchor);
      else if (target && handle.focusAtCoords) handle.focusAtCoords(target);
      else handle.focus();
    },
    [editorRef],
  );

  const activate = useCallback(
    (target?: LazyFocusTarget) => {
      focusTargetRef.current = target ?? null;
      focusPendingRef.current = true;
      setActive(true);
      if (ready) {
        focusPendingRef.current = false;
        const latched = focusTargetRef.current;
        focusTargetRef.current = null;
        focusAtTarget(latched);
      }
    },
    [ready, focusAtTarget],
  );

  const onReady = useCallback(() => setReady(true), []);

  useEffect(() => {
    if (!ready) return;
    if (focusPendingRef.current) {
      focusPendingRef.current = false;
      const target = focusTargetRef.current;
      focusTargetRef.current = null;
      focusAtTarget(target);
    }
    const pending = pendingFilesRef.current;
    pendingFilesRef.current = [];
    for (const file of pending) editorRef.current?.uploadFile?.(file);
  }, [ready, editorRef, focusAtTarget]);

  const uploadOrQueue = useCallback(
    (files: File[]) => {
      if (ready) {
        for (const file of files) editorRef.current?.uploadFile?.(file);
        return;
      }
      pendingFilesRef.current.push(...files);
      setActive(true);
    },
    [ready, editorRef],
  );

  return { active, ready, activate, onReady, uploadOrQueue };
}
