// Расширение копирования: text/plain в буфере несёт Markdown-исходник,
// а не textContent. Симметрично markdown-paste.ts.
import { Extension } from '@tiptap/core';
import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { Slice } from '@tiptap/pm/model';

const BLOB_IMAGE_RE = /!\[[^\]]*\]\(blob:[^)]*\)\n?/g;

export function createMarkdownCopyExtension() {
  return Extension.create({
    name: 'markdownCopy',
    addProseMirrorPlugins() {
      const { editor } = this;

      const fallback = (slice: Slice) => slice.content.textBetween(0, slice.content.size, '\n\n');

      return [
        new Plugin({
          key: new PluginKey('markdownCopy'),
          props: {
            clipboardTextSerializer(slice: Slice) {
              if (!editor.markdown) return fallback(slice);
              try {
                const doc = editor.schema.topNodeType.create(null, slice.content);
                const md = editor.markdown.serialize(doc.toJSON());
                return md.replace(BLOB_IMAGE_RE, '').replace(/\n+$/, '');
              } catch {
                return fallback(slice);
              }
            },
          },
        }),
      ];
    },
  });
}
