import { Extension } from '@tiptap/core';
import { Plugin } from '@tiptap/pm/state';
import {
  getShortcut,
  isPlainShortcut,
  shortcutMatchesEvent,
  type ShortcutChord,
} from '@goosar/core/shortcuts';
import { isImeComposing } from '@goosar/core/utils';

export function shouldHandleSubmitShortcut(
  event: KeyboardEvent,
  options: {
    configuredShortcut: ShortcutChord | null;
    composing: boolean;
  },
): boolean {
  return (
    !event.repeat &&
    !options.composing &&
    !isImeComposing(event) &&
    shortcutMatchesEvent(options.configuredShortcut, event)
  );
}

export function shouldReplayNativeEnter(
  event: KeyboardEvent,
  configuredShortcut: ShortcutChord | null,
  composing: boolean,
): boolean {
  return (
    !composing &&
    !isImeComposing(event) &&
    isPlainShortcut(configuredShortcut, 'Enter') &&
    event.key === 'Enter' &&
    event.shiftKey &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey
  );
}

export function createSubmitShortcutExtension(onSubmit: () => boolean) {
  return Extension.create({
    name: 'submitShortcut',
    priority: 100,
    addProseMirrorPlugins() {
      const editor = this.editor;
      let replayingEnter = false;
      return [
        new Plugin({
          props: {
            handleKeyDown(view, event) {
              if (replayingEnter) return false;
              const shortcut = getShortcut('send');

              if (shouldReplayNativeEnter(event, shortcut, view.composing)) {
                replayingEnter = true;
                try {
                  return editor.commands.keyboardShortcut('Enter');
                } finally {
                  replayingEnter = false;
                }
              }

              if (
                !shouldHandleSubmitShortcut(event, {
                  configuredShortcut: shortcut,
                  composing: view.composing,
                })
              ) {
                return false;
              }
              return onSubmit();
            },
          },
        }),
      ];
    },
  });
}
