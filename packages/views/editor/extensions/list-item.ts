import { type Editor, InputRule } from '@tiptap/core';
import { ListItem, TaskItem } from '@tiptap/extension-list';
import { sinkListItem as pmSinkListItem } from '@tiptap/pm/schema-list';
import { type Command, TextSelection } from '@tiptap/pm/state';
import type { NodeType } from '@tiptap/pm/model';

function sinkListItemRange(itemType: NodeType): Command {
  return (state, dispatch) => {
    if (pmSinkListItem(itemType)(state, dispatch)) return true;

    const { $from, $to } = state.selection;
    const range = $from.blockRange(
      $to,
      (node) => node.childCount > 0 && node.firstChild?.type === itemType,
    );
    if (!range) return false;
    if (range.startIndex !== 0) return false;
    if (range.endIndex - range.startIndex < 2) return false;
    if (range.parent.child(range.startIndex).type !== itemType) return false;

    const secondItemStart = range.start + range.parent.child(range.startIndex).nodeSize + 2;
    const narrowed = state.apply(
      state.tr.setSelection(TextSelection.between(state.doc.resolve(secondItemStart), $to)),
    );
    return pmSinkListItem(itemType)(narrowed, dispatch);
  };
}

function listItemKeymap(editor: Editor, name: string) {
  return {
    Enter: () =>
      editor.commands.first(({ commands }) => [
        () => commands.splitListItem(name),
        () => commands.liftListItem(name),
      ]),
    Tab: () => {
      const itemType = editor.schema.nodes[name];
      if (!itemType) return false;
      sinkListItemRange(itemType)(editor.state, (tr) => editor.view.dispatch(tr));
      return editor.isActive(name);
    },
    'Shift-Tab': () => editor.commands.liftListItem(name),
  };
}

export const PatchedListItem = ListItem.extend({
  addKeyboardShortcuts() {
    return listItemKeymap(this.editor, this.name);
  },
});

export const PatchedTaskItem = TaskItem.extend({
  addKeyboardShortcuts() {
    return listItemKeymap(this.editor, this.name);
  },
  addInputRules() {
    return [
      ...(this.parent?.() ?? []),
      new InputRule({
        find: /^\[([ xX])?\]\s$/,
        handler: ({ state, range, match, chain }) => {
          if (state.selection.$from.node(-1)?.type.name === 'listItem') {
            const checked = (match[1] ?? '').toLowerCase() === 'x';
            chain()
              .deleteRange(range)
              .liftListItem('listItem')
              .toggleList('taskList', 'taskItem')
              .updateAttributes('taskItem', { checked })
              .run();
          }
        },
      }),
    ];
  },
}).configure({ nested: true });
