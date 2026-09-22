'use client';

import { Node, mergeAttributes } from '@tiptap/core';
import { ReactNodeViewRenderer, NodeViewWrapper } from '@tiptap/react';
import type { NodeViewProps } from '@tiptap/react';
import { FILE_CARD_URL_PATTERN } from '@goosar/ui/markdown';
import { escapeMarkdownLabel } from '../utils/escape-markdown-label';
import { Attachment } from '../attachment';

const FILE_CARD_MARKDOWN_RE = new RegExp(
  `^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)`,
);

export function FileCardView({ node, editor, deleteNode }: NodeViewProps) {
  const href = (node.attrs.href as string) || '';
  const filename = (node.attrs.filename as string) || '';
  const uploading = node.attrs.uploading as boolean;
  const editable = editor?.isEditable ?? false;

  return (
    <NodeViewWrapper as="div" className="file-card-node" data-type="fileCard">
      <div contentEditable={false}>
        <Attachment
          attachment={{ kind: 'url', url: href, filename, uploading }}
          editable={editable}
          onDelete={editable ? deleteNode : undefined}
        />
      </div>
    </NodeViewWrapper>
  );
}

export const FileCardExtension = Node.create({
  name: 'fileCard',
  group: 'block',
  atom: true,

  addAttributes() {
    return {
      href: {
        default: '',
        rendered: false, // Don't put href on DOM — prevents link behavior
      },
      filename: {
        default: '',
        rendered: false,
      },
      fileSize: {
        default: 0,
        rendered: false,
      },
      uploading: {
        default: false,
        rendered: false,
      },
      uploadId: {
        default: null,
        rendered: false,
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'div[data-type="fileCard"]',
        getAttrs: (el) => ({
          href: (el as HTMLElement).getAttribute('data-href'),
          filename: (el as HTMLElement).getAttribute('data-filename'),
        }),
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    return [
      'div',
      mergeAttributes(HTMLAttributes, {
        'data-type': 'fileCard',
        'data-href': node.attrs.href,
        'data-filename': node.attrs.filename,
      }),
    ];
  },

  markdownTokenizer: {
    name: 'fileCard',
    level: 'block' as const,
    start(src: string) {
      return src.search(/^!file\[/m);
    },
    tokenize(src: string) {
      const match = src.match(FILE_CARD_MARKDOWN_RE);
      if (!match) return undefined;
      const filename = (match[1] ?? '').replace(/\\([[\]\\()])/g, '$1');
      return {
        type: 'fileCard',
        raw: match[0],
        attributes: { filename, href: match[2] },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode('fileCard', token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { href, filename, uploading } = node.attrs || {};
    if (uploading === true || !href) return '';
    return `!file[${escapeMarkdownLabel(filename || 'file')}](${href})`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(FileCardView);
  },
});
