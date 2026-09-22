'use client';

import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react';
import { useEditor, EditorContent } from '@tiptap/react';
import { Extension } from '@tiptap/core';
import { Document } from '@tiptap/extension-document';
import { Paragraph } from '@tiptap/extension-paragraph';
import { Text } from '@tiptap/extension-text';
import Placeholder from '@tiptap/extension-placeholder';
import { getShortcut, isPlainShortcut, type ShortcutChord } from '@goosar/core/shortcuts';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../i18n';
import { createSubmitShortcutExtension } from './extensions/submit-shortcut';
import './title-editor.css';

interface TitleEditorProps {
  defaultValue?: string;
  placeholder?: string;
  className?: string;
  autoFocus?: boolean;
  onSubmit?: () => void;
  onSubmitShortcut?: () => void;
  onBlur?: (value: string) => void;
  onChange?: (value: string) => void;
  onReady?: () => void;
}

interface TitleEditorRef {
  getText: () => string;
  focus: () => void;
  focusAtCoords: (coords: { x: number; y: number }) => void;
}

const SingleLineDocument = Document.extend({
  content: 'paragraph',
});

export function titleShortcutSubmitAllowed(sendShortcut: ShortcutChord | null): boolean {
  if (!sendShortcut) return false;
  return !isPlainShortcut(sendShortcut, 'Enter');
}

function createTitleKeymap(opts: { onSubmitRef: React.RefObject<(() => void) | undefined> }) {
  return Extension.create({
    name: 'titleKeymap',
    addKeyboardShortcuts() {
      return {
        Enter: ({ editor }) => {
          opts.onSubmitRef.current?.();
          editor.commands.blur();
          return true;
        },
        'Shift-Enter': () => true, // swallow — no line breaks
        Escape: ({ editor }) => {
          editor.commands.blur();
          return true;
        },
      };
    },
  });
}

const TitleEditor = forwardRef<TitleEditorRef, TitleEditorProps>(function TitleEditor(
  {
    defaultValue = '',
    placeholder: placeholderText = '',
    className,
    autoFocus = false,
    onSubmit,
    onSubmitShortcut,
    onBlur,
    onChange,
    onReady,
  },
  ref,
) {
  const { t } = useT('editor');
  const onSubmitRef = useRef(onSubmit);
  const onSubmitShortcutRef = useRef(onSubmitShortcut);
  const onBlurRef = useRef(onBlur);
  const onChangeRef = useRef(onChange);
  const onReadyRef = useRef(onReady);

  onSubmitRef.current = onSubmit;
  onSubmitShortcutRef.current = onSubmitShortcut;
  onBlurRef.current = onBlur;
  onChangeRef.current = onChange;
  onReadyRef.current = onReady;

  const shortcutSubmitEnabled = useRef(onSubmitShortcut !== undefined).current;

  const editor = useEditor({
    immediatelyRender: false,
    content: defaultValue
      ? {
          type: 'doc',
          content: [{ type: 'paragraph', content: [{ type: 'text', text: defaultValue }] }],
        }
      : '',
    extensions: [
      SingleLineDocument,
      Paragraph,
      Text,
      Placeholder.configure({
        placeholder: placeholderText,
        showOnlyCurrent: false,
      }),
      createTitleKeymap({ onSubmitRef }),
      ...(shortcutSubmitEnabled
        ? [
            createSubmitShortcutExtension(() => {
              const fn = onSubmitShortcutRef.current;
              if (!fn) return false;
              if (!titleShortcutSubmitAllowed(getShortcut('send'))) return false;
              fn();
              return true;
            }),
          ]
        : []),
    ],
    editorProps: {
      attributes: {
        class: cn('title-editor outline-none', className),
        role: 'textbox',
        'aria-multiline': 'false',
        'aria-label': placeholderText || t(($) => $.title_editor.title_aria_label),
      },
    },
    onUpdate: ({ editor: ed }) => {
      onChangeRef.current?.(ed.getText());
    },
    onBlur: ({ editor: ed }) => {
      onBlurRef.current?.(ed.getText());
    },
  });

  const readyFiredRef = useRef(false);
  useEffect(() => {
    if (!editor || readyFiredRef.current) return;
    readyFiredRef.current = true;
    onReadyRef.current?.();
  }, [editor]);

  useEffect(() => {
    if (autoFocus && editor) {
      const timer = setTimeout(() => {
        editor.commands.focus('end');
      }, 50);
      return () => clearTimeout(timer);
    }
    return undefined;
  }, [autoFocus, editor]);

  const lastDefaultValueRef = useRef(defaultValue);

  useEffect(() => {
    if (!editor || editor.isDestroyed) return;
    const prevDefaultValue = lastDefaultValueRef.current;
    lastDefaultValueRef.current = defaultValue;

    if (editor.getText() === defaultValue) return;

    if (editor.isFocused && editor.getText() !== prevDefaultValue) return;

    editor.commands.setContent(
      defaultValue
        ? {
            type: 'doc',
            content: [{ type: 'paragraph', content: [{ type: 'text', text: defaultValue }] }],
          }
        : '',
      { emitUpdate: false },
    );
  }, [defaultValue, editor]);

  useImperativeHandle(ref, () => ({
    getText: () => editor?.getText() ?? '',
    focus: () => {
      editor?.commands.focus('end');
    },
    focusAtCoords: (coords: { x: number; y: number }) => {
      if (!editor) return;
      const pos = editor.view.posAtCoords({ left: coords.x, top: coords.y });
      if (pos) editor.commands.focus(pos.pos);
      else editor.commands.focus('end');
    },
  }));

  if (!editor) return null;

  return <EditorContent editor={editor} />;
});

export { TitleEditor, type TitleEditorProps, type TitleEditorRef };
