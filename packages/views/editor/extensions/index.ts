// Общая фабрика расширений ContentEditor: один набор для режимов правки
// и чтения, чтобы контент рендерился одинаково.
import type { RefObject } from 'react';
import StarterKit from '@tiptap/starter-kit';
import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight';
import Placeholder from '@tiptap/extension-placeholder';
import Link from '@tiptap/extension-link';
import Typography from '@tiptap/extension-typography';
import Image from '@tiptap/extension-image';
import TableRow from '@tiptap/extension-table-row';
import TableHeader from '@tiptap/extension-table-header';
import TableCell from '@tiptap/extension-table-cell';
import { Table } from '@tiptap/extension-table';
import { TaskList } from '@tiptap/extension-list';
import { Markdown } from '@tiptap/markdown';
import { ReactNodeViewRenderer } from '@tiptap/react';
import type { AnyExtension } from '@tiptap/core';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import { shouldAutoLink } from '@goosar/ui/markdown';
import { escapeMarkdownLabel } from '../utils/escape-markdown-label';
import { BaseMentionExtension } from './mention-extension';
import { createMentionSuggestion, type MentionItem } from './mention-suggestion';
import {
  createIssueIdentifierAutolinkExtension,
  type IssueIdentifierResolver,
} from './issue-identifier-autolink';
import { SlashCommandExtension } from './slash-command-extension';
import {
  createSlashCommandSuggestion,
  createBuiltinCommandSuggestion,
} from './slash-command-suggestion';
import { CodeBlockView } from './code-block-view';
import { PatchedListItem, PatchedTaskItem } from './list-item';
import { createMarkdownPasteExtension } from './markdown-paste';
import { createMarkdownCopyExtension } from './markdown-copy';
import { createBlurShortcutExtension } from './blur-shortcut';
import { createSubmitShortcutExtension } from './submit-shortcut';
import { createFileUploadExtension } from './file-upload';
import { FileCardExtension } from './file-card';
import { ImageView } from './image-view';
import { BlockMathExtension, InlineMathExtension } from './math';
import { HighlightExtension } from './highlight';
import { codeLowlight } from '../syntax-highlight';

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: 'https',
  shouldAutoLink,
});

export const ImageExtension = Image.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      uploading: {
        default: false,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.uploading ? { 'data-uploading': '' } : {},
        parseHTML: (el: HTMLElement) => el.hasAttribute('data-uploading'),
      },
      uploadId: {
        default: null,
        rendered: false,
      },
      width: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.width ? { width: attrs.width as number } : {},
        parseHTML: (el: HTMLElement) => {
          const w = parseInt(el.getAttribute('width') || '', 10);
          return Number.isFinite(w) ? w : null;
        },
      },
      height: {
        default: null,
        renderHTML: (attrs: Record<string, unknown>) =>
          attrs.height ? { height: attrs.height as number } : {},
        parseHTML: (el: HTMLElement) => {
          const h = parseInt(el.getAttribute('height') || '', 10);
          return Number.isFinite(h) ? h : null;
        },
      },
    };
  },
  addNodeView() {
    return ReactNodeViewRenderer(ImageView);
  },
  renderMarkdown: (node: any) => {
    const src = node.attrs?.src || '';
    if (node.attrs?.uploading === true || !src) return '';
    const alt = escapeMarkdownLabel(node.attrs?.alt || '');
    const title = node.attrs?.title;
    if (title) {
      return `![${alt}](${src} "${title}")`;
    }
    return `![${alt}](${src})`;
  },
}).configure({
  inline: false,
  allowBase64: false,
});

export interface EditorExtensionsOptions {
  placeholder?: string | (() => string);
  queryClient?: import('@tanstack/react-query').QueryClient;
  onSubmitRef?: RefObject<(() => void) | undefined>;
  onUploadFileRef?: RefObject<
    ((file: File, uploadId: string) => Promise<UploadResult | null>) | undefined
  >;
  pasteAsFileThresholdRef?: RefObject<number | undefined>;
  disableMentions?: boolean;
  mentionMode?: 'default' | 'context';
  getMentionContextItems?: () => MentionItem[];
  enableSlashCommands?: boolean;
  slashCommandMode?: 'skill' | 'command';
  resolveIssueIdentifierRef?: RefObject<IssueIdentifierResolver | undefined>;
}

export function createEditorExtensions(options: EditorExtensionsOptions): AnyExtension[] {
  const { placeholder: placeholderText } = options;

  return [
    StarterKit.configure({
      heading: { levels: [1, 2, 3] },
      link: false,
      codeBlock: false,
      underline: false,
      listItem: false,
    }),
    PatchedListItem,
    TaskList,
    PatchedTaskItem,
    CodeBlockLowlight.extend({
      addNodeView() {
        return ReactNodeViewRenderer(CodeBlockView);
      },
    }).configure({ lowlight: codeLowlight }),
    LinkExtension,
    ImageExtension,
    Table.configure({ resizable: false, renderWrapper: true }),
    TableRow,
    TableHeader,
    TableCell,
    BlockMathExtension,
    InlineMathExtension,
    HighlightExtension,
    Markdown.configure({ indentation: { style: 'space', size: 3 } }),
    createMarkdownCopyExtension(),
    FileCardExtension,
    BaseMentionExtension.configure({
      HTMLAttributes: { class: 'mention' },
      ...(options.disableMentions
        ? { suggestion: { allow: () => false } }
        : options.queryClient
          ? {
              suggestion: createMentionSuggestion(options.queryClient, {
                mode: options.mentionMode,
                getContextItems: options.getMentionContextItems,
              }),
            }
          : {}),
    }),
    ...(!options.disableMentions && options.resolveIssueIdentifierRef
      ? [
          createIssueIdentifierAutolinkExtension({
            resolveRef: options.resolveIssueIdentifierRef,
          }),
        ]
      : []),
    SlashCommandExtension.configure({
      HTMLAttributes: { class: 'slash-command' },
      suggestion: !options.enableSlashCommands
        ? { char: '/', allow: () => false }
        : options.slashCommandMode === 'command'
          ? createBuiltinCommandSuggestion()
          : options.queryClient
            ? createSlashCommandSuggestion(options.queryClient)
            : { char: '/', allow: () => false },
    }),
    Typography,
    Placeholder.configure({ placeholder: placeholderText }),
    createMarkdownPasteExtension(),
    createSubmitShortcutExtension(() => {
      const fn = options.onSubmitRef?.current;
      if (!fn) return false;
      fn();
      return true;
    }),
    createBlurShortcutExtension(),
    createFileUploadExtension(options.onUploadFileRef!, options.pasteAsFileThresholdRef),
  ];
}
