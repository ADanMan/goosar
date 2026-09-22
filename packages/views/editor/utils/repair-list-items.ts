import { TextSelection } from '@tiptap/pm/state';
import type { Editor } from '@tiptap/core';

interface JsonNode {
  type?: string;
  content?: JsonNode[];
  [key: string]: unknown;
}

function repairListItems(node: JsonNode): { node: JsonNode; changed: boolean } {
  let changed = false;
  let content = node.content;

  if (content) {
    const mapped = content.map((child) => {
      const result = repairListItems(child);
      if (result.changed) changed = true;
      return result.node;
    });
    if (changed) content = mapped;
  }

  if (node.type === 'listItem' || node.type === 'taskItem') {
    if (!content || content.length === 0 || content[0]?.type !== 'paragraph') {
      content = [{ type: 'paragraph' }, ...(content ?? [])];
      changed = true;
    }
  }

  return changed ? { node: { ...node, content }, changed } : { node, changed };
}

export function repairEmptyListItems(
  editor: Editor,
  preferredSelection?: { from: number; to: number },
): boolean {
  if (editor.isDestroyed) return false;

  const { node: repaired, changed } = repairListItems(editor.getJSON() as JsonNode);
  if (!changed) return false;

  editor.view.dispatch(
    editor.state.tr
      .setSelection(TextSelection.near(editor.state.doc.resolve(0)))
      .setMeta('addToHistory', false),
  );

  editor.chain().setMeta('addToHistory', false).setContent(repaired, { emitUpdate: false }).run();

  const { doc } = editor.state;
  const size = doc.content.size;
  const clamp = (pos: number) => Math.min(Math.max(pos, 0), size);
  const anchor = preferredSelection ? clamp(preferredSelection.from) : 0;
  const head = preferredSelection ? clamp(preferredSelection.to) : 0;
  editor.view.dispatch(
    editor.state.tr
      .setSelection(TextSelection.between(doc.resolve(anchor), doc.resolve(head)))
      .setMeta('addToHistory', false),
  );
  return true;
}
