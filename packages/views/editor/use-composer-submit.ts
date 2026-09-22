'use client';

import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';
import type { ContentEditorRef } from './content-editor';
import type { UploadGate } from './use-upload-gate';

export type ComposerAfterAccepted = 'refocus' | 'blur' | 'none';

export interface ComposerSubmitOptions {
  editorRef: RefObject<ContentEditorRef | null>;
  uploadGate: UploadGate;
  onSubmit: (content: string) => Promise<boolean>;
  onAccepted?: () => void;
  normalize?: (raw: string) => string;
  afterAccepted?: ComposerAfterAccepted | (() => ComposerAfterAccepted);
  containerRef?: RefObject<HTMLElement | null>;
}

export interface ComposerSubmit {
  submitting: boolean;
  submit: () => Promise<void>;
}

const defaultNormalize = (raw: string) => raw.replace(/(\n\s*)+$/, '').trim();

export function useComposerSubmit(opts: ComposerSubmitOptions): ComposerSubmit {
  const [submitting, setSubmitting] = useState(false);
  const inFlight = useRef(false);
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const mountedRef = useRef(true);
  const focusFrameRef = useRef<number | null>(null);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (focusFrameRef.current !== null) {
        cancelAnimationFrame(focusFrameRef.current);
        focusFrameRef.current = null;
      }
    };
  }, []);

  const runAfterAccepted = useCallback(() => {
    const o = optsRef.current;
    const mode =
      typeof o.afterAccepted === 'function' ? o.afterAccepted() : (o.afterAccepted ?? 'none');
    if (mode === 'none') return;
    if (focusFrameRef.current !== null) cancelAnimationFrame(focusFrameRef.current);
    focusFrameRef.current = requestAnimationFrame(() => {
      focusFrameRef.current = null;
      if (!mountedRef.current) return;
      if (mode === 'blur') {
        optsRef.current.editorRef.current?.blur();
        return;
      }
      const container = optsRef.current.containerRef?.current;
      const active = typeof document === 'undefined' ? null : document.activeElement;
      const ownsFocus =
        !container || !active || active === document.body || container.contains(active);
      if (!ownsFocus) return;
      optsRef.current.editorRef.current?.focus();
    });
  }, []);

  const submit = useCallback(async () => {
    const o = optsRef.current;
    const raw = o.editorRef.current?.getMarkdown() ?? '';
    const content = (o.normalize ?? defaultNormalize)(raw);
    if (!content || inFlight.current) return;
    if (o.uploadGate.isBlocked()) return;

    inFlight.current = true;
    setSubmitting(true);
    try {
      const accepted = await o.onSubmit(content);
      if (accepted) {
        o.onAccepted?.();
        runAfterAccepted();
      }
    } catch {
      // A thrown send is a rejection: keep the draft, let the caller's
      // onSubmit surface its own error toast.
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }, [runAfterAccepted]);

  return { submitting, submit };
}
