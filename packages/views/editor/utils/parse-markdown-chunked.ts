import type { JSONContent } from '@tiptap/core';

export const MARKDOWN_CHUNK_THRESHOLD = 8_000;

export const MARKDOWN_CHUNK_SIZE = 4_000;

export interface MarkdownManagerLike {
  parse(markdown: string): JSONContent;
}

export function parseMarkdownChunked(
  manager: MarkdownManagerLike,
  markdown: string,
  chunkSize = MARKDOWN_CHUNK_SIZE,
): JSONContent {
  const lines = markdown.split('\n');
  const chunks: string[] = [];
  let current: string[] = [];
  let currentLen = 0;
  let inFence = false;

  for (const line of lines) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    current.push(line);
    currentLen += line.length + 1;

    if (currentLen >= chunkSize && !inFence && line.trim() === '') {
      chunks.push(current.join('\n'));
      current = [];
      currentLen = 0;
    }
  }
  if (current.length) chunks.push(current.join('\n'));

  const merged: JSONContent = { type: 'doc', content: [] };
  for (const chunk of chunks) {
    const doc = manager.parse(chunk);
    if (doc.content) merged.content!.push(...doc.content);
  }
  return merged;
}
