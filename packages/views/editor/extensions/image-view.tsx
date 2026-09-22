'use client';

import { NodeViewWrapper } from '@tiptap/react';
import type { NodeViewProps } from '@tiptap/react';
import { Attachment } from '../attachment';

function ImageView({ node, editor, selected, deleteNode }: NodeViewProps) {
  const src = (node.attrs.src as string) || '';
  const alt = (node.attrs.alt as string) || '';
  const uploading = node.attrs.uploading as boolean;
  const width = (node.attrs.width as number | null) ?? undefined;
  const height = (node.attrs.height as number | null) ?? undefined;

  return (
    <NodeViewWrapper>
      <Attachment
        attachment={{
          kind: 'url',
          url: src,
          filename: alt,
          uploading,
          width,
          height,
          forceKind: 'image',
        }}
        editable={editor.isEditable}
        selected={selected}
        onDelete={() => deleteNode()}
      />
    </NodeViewWrapper>
  );
}

export { ImageView };
