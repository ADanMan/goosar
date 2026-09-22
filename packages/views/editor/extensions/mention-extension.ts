import Mention from '@tiptap/extension-mention';
import { mergeAttributes } from '@tiptap/core';
import { ReactNodeViewRenderer } from '@tiptap/react';
import { MentionView } from './mention-view';
import { escapeMarkdownLabel } from '../utils/escape-markdown-label';

const MENTION_LINK_MARKER = '](mention://';

function isEscaped(text: string, index: number): boolean {
  let slashCount = 0;
  for (let i = index - 1; i >= 0 && text[i] === '\\'; i--) {
    slashCount++;
  }
  return slashCount % 2 === 1;
}

function findUnescapedMentionMarker(src: string, from = 0): number {
  let marker = src.indexOf(MENTION_LINK_MARKER, from);

  while (marker !== -1) {
    if (!isEscaped(src, marker)) return marker;
    marker = src.indexOf(MENTION_LINK_MARKER, marker + MENTION_LINK_MARKER.length);
  }

  return -1;
}

function findMentionStart(src: string): number {
  let marker = findUnescapedMentionMarker(src);

  while (marker !== -1) {
    for (let i = marker - 1; i >= 0; i--) {
      const char = src[i];
      if (char === '\n' || char === '\r') break;
      if (char === '[' && !isEscaped(src, i)) return i;
    }

    marker = findUnescapedMentionMarker(src, marker + MENTION_LINK_MARKER.length);
  }

  return -1;
}

export const BaseMentionExtension = Mention.extend({
  addNodeView() {
    return ReactNodeViewRenderer(MentionView);
  },
  renderHTML({ node, HTMLAttributes }) {
    const type = node.attrs.type ?? 'member';
    const prefix = type === 'issue' || type === 'project' ? '' : '@';
    return [
      'span',
      mergeAttributes({ 'data-type': 'mention' }, this.options.HTMLAttributes, HTMLAttributes, {
        'data-mention-type': node.attrs.type ?? 'member',
        'data-mention-id': node.attrs.id,
      }),
      `${prefix}${node.attrs.label ?? node.attrs.id}`,
    ];
  },
  addAttributes() {
    return {
      ...this.parent?.(),
      type: {
        default: 'member',
        parseHTML: (el: HTMLElement) => el.getAttribute('data-mention-type') ?? 'member',
        renderHTML: () => ({}),
      },
    };
  },
  markdownTokenizer: {
    name: 'mention',
    level: 'inline' as const,
    start(src: string) {
      return findMentionStart(src);
    },
    tokenize(src: string) {
      const match = src.match(/^\[@?((?:\\.|[^\]\\])+)\]\(mention:\/\/(\w+)\/([^)]+)\)/);
      if (!match) return undefined;
      const rawLabel = match[1]?.replace(/\\([[\]\\()])/g, '$1');
      return {
        type: 'mention',
        raw: match[0],
        attributes: { label: rawLabel, type: match[2] ?? 'member', id: match[3] },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode('mention', token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { id, label, type = 'member' } = node.attrs || {};
    const prefix = type === 'issue' || type === 'project' ? '' : '@';
    const safeLabel = escapeMarkdownLabel(label ?? id);
    return `[${prefix}${safeLabel}](mention://${type}/${id})`;
  },
});
