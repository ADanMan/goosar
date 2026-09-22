import { afterEach, describe, expect, it } from 'vitest';
import { Editor } from '@tiptap/core';
import { createEditorExtensions } from '.';

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = '';
});

function makeProductionEditor(): Editor {
  const element = document.createElement('div');
  document.body.appendChild(element);

  return new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: '',
      disableMentions: true,
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
    }),
  });
}

function paste(ed: Editor, text: string, html: string): void {
  const event = new Event('paste', { bubbles: false, cancelable: true });
  Object.defineProperty(event, 'clipboardData', {
    value: {
      files: [],
      getData: (type: string) => (type === 'text/plain' ? text : type === 'text/html' ? html : ''),
    },
  });
  ed.view.dom.dispatchEvent(event);
}

describe('underline is not in the editor schema', () => {
  it('registers no underline mark, so Cmd+U cannot produce one', () => {
    editor = makeProductionEditor();

    expect(editor.schema.marks.underline).toBeUndefined();
  });
});

describe('pasting underlined rich text', () => {
  it('keeps the text of a pasted <u> tag and writes no ++ delimiters', () => {
    editor = makeProductionEditor();

    paste(
      editor,
      'before underlined after',
      '<meta charset="utf-8"><p>before <u>underlined</u> after</p>',
    );

    expect(editor.getText()).toContain('underlined');
    expect(editor.getMarkdown()).toContain('underlined');
    expect(editor.getMarkdown()).not.toContain('++');
  });

  it('keeps the text of a pasted text-decoration: underline span and writes no ++ delimiters', () => {
    editor = makeProductionEditor();

    paste(
      editor,
      'before underlined after',
      '<meta charset="utf-8"><p>before <span style="text-decoration: underline">underlined</span> after</p>',
    );

    expect(editor.getText()).toContain('underlined');
    expect(editor.getMarkdown()).toContain('underlined');
    expect(editor.getMarkdown()).not.toContain('++');
  });

  it('preserves sibling formatting when dropping the underline mark', () => {
    editor = makeProductionEditor();

    paste(
      editor,
      'bold underlined',
      '<meta charset="utf-8"><p><strong>bold</strong> <u>underlined</u></p>',
    );

    const markdown = editor.getMarkdown();
    expect(markdown).toContain('**bold**');
    expect(markdown).toContain('underlined');
    expect(markdown).not.toContain('++');
  });
});

describe('existing content containing ++', () => {
  it('round-trips literal ++ text unchanged', () => {
    editor = makeProductionEditor();
    editor.commands.setContent(editor.markdown!.parse('a ++b++ c'));

    expect(editor.getText()).toBe('a ++b++ c');
    expect(editor.getMarkdown().trim()).toBe('a ++b++ c');
  });
});
