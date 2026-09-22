import { Extension } from '@tiptap/core';

export function createBlurShortcutExtension() {
  return Extension.create({
    name: 'blurShortcut',
    addKeyboardShortcuts() {
      return {
        Escape: ({ editor }) => {
          editor.commands.blur();
          return true;
        },
      };
    },
  });
}
