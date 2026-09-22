// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { Schema } from '@tiptap/pm/model';
import { anchorFromPoint, posFromAnchor } from './text-anchor';

const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: { group: 'block', content: 'inline*' },
    codeBlock: { group: 'block', content: 'text*', marks: '' },
    bulletList: { group: 'block', content: 'listItem+' },
    listItem: { content: 'paragraph+' },
    mention: { group: 'inline', inline: true, atom: true },
    text: { group: 'inline' },
  },
});

const text = (s: string) => schema.text(s);
const p = (...content: ReturnType<typeof text>[]) => schema.node('paragraph', null, content);

describe('posFromAnchor', () => {
  it('lands before the next word or after the previous one, per bias', () => {
    const doc = schema.node('doc', null, [p(text('hello world'))]);
    expect(posFromAnchor(doc, { block: 0, offset: 5, bias: 1 })).toBe(7); 
    expect(posFromAnchor(doc, { block: 0, offset: 5, bias: -1 })).toBe(6); 
  });

  it('maps offsets in later blocks past preceding node sizes', () => {
    const doc = schema.node('doc', null, [p(text('abc')), p(text('defgh'))]);
    expect(posFromAnchor(doc, { block: 1, offset: 2, bias: 1 })).toBe(8);
    expect(posFromAnchor(doc, { block: 1, offset: 2, bias: -1 })).toBe(8);
  });

  it('skips zero-text inline atoms without counting them', () => {
    const doc = schema.node('doc', null, [p(text('hi '), schema.node('mention'), text(' yo'))]);
    expect(posFromAnchor(doc, { block: 0, offset: 3, bias: 1 })).toBe(7);
    expect(posFromAnchor(doc, { block: 0, offset: 4, bias: 1 })).toBe(8);
  });

  it("clamps an overshooting offset to the block's content end", () => {
    const doc = schema.node('doc', null, [p(text('abc')), p(text('xyz'))]);
    expect(posFromAnchor(doc, { block: 0, offset: 99, bias: 1 })).toBe(4);
  });

  it('clamps an out-of-range block index to the document end', () => {
    const doc = schema.node('doc', null, [p(text('abc'))]);
    expect(posFromAnchor(doc, { block: 7, offset: 0, bias: 1 })).toBe(doc.content.size);
  });

  it('distinguishes line end from next line start in code blocks', () => {
    const doc = schema.node('doc', null, [
      p(text('intro')),
      schema.node('codeBlock', null, [text('line1\nline2')]),
    ]);
    expect(posFromAnchor(doc, { block: 1, offset: 5, bias: -1 })).toBe(13); 
    expect(posFromAnchor(doc, { block: 1, offset: 5, bias: 1 })).toBe(14); 
  });
});

describe('anchorFromPoint', () => {
  const docAny = document as unknown as {
    caretPositionFromPoint?: (
      x: number,
      y: number,
    ) => {
      offsetNode: Node;
      offset: number;
    } | null;
  };
  const originalCaret = docAny.caretPositionFromPoint;

  afterEach(() => {
    docAny.caretPositionFromPoint = originalCaret;
    document.body.innerHTML = '';
  });

  function mount(html: string): HTMLElement {
    const root = document.createElement('div');
    root.innerHTML = html;
    document.body.appendChild(root);
    return root;
  }

  it('counts non-whitespace chars before the caret and biases toward text', () => {
    const root = mount('<p>abc</p><p><strong>de</strong>fgh</p>');
    const second = root.children[1] as HTMLElement;
    docAny.caretPositionFromPoint = () => ({
      offsetNode: second.childNodes[1] as Node,
      offset: 1,
    });
    expect(anchorFromPoint(0, 0, root)).toEqual({ block: 1, offset: 3, bias: 1 });
  });

  it('biases backward when the caret sits at the end of the text', () => {
    const root = mount('<p>abc</p>');
    const para = root.children[0] as HTMLElement;
    docAny.caretPositionFromPoint = () => ({
      offsetNode: para.firstChild as Node,
      offset: 3,
    });
    expect(anchorFromPoint(0, 0, root)).toEqual({ block: 0, offset: 3, bias: -1 });
  });

  it('ignores inter-element whitespace artifacts (the list drift bug)', () => {
    const root = mount('<ul><li>URL: x</li>\n<li>Title: feat</li></ul>');
    const li2 = root.querySelector('li:nth-child(2)') as HTMLElement;
    docAny.caretPositionFromPoint = () => ({
      offsetNode: li2.firstChild as Node,
      offset: 7, // "Title: ▮feat"
    });
    const anchor = anchorFromPoint(0, 0, root);
    expect(anchor).toEqual({ block: 0, offset: 11, bias: 1 });

    const doc = schema.node('doc', null, [
      schema.node('bulletList', null, [
        schema.node('listItem', null, [p(text('URL: x'))]),
        schema.node('listItem', null, [p(text('Title: feat'))]),
      ]),
    ]);
    const pos = posFromAnchor(doc, anchor!);
    expect(doc.textBetween(pos, pos + 4)).toBe('feat');
  });

  it('returns null when the caret lands outside the root', () => {
    const root = mount('<p>abc</p>');
    const stranger = document.createElement('p');
    stranger.textContent = 'elsewhere';
    document.body.appendChild(stranger);
    docAny.caretPositionFromPoint = () => ({
      offsetNode: stranger.firstChild as Node,
      offset: 0,
    });
    expect(anchorFromPoint(0, 0, root)).toBeNull();
  });

  it('returns null when no caret API is available', () => {
    const root = mount('<p>abc</p>');
    docAny.caretPositionFromPoint = undefined;
    expect(anchorFromPoint(0, 0, root)).toBeNull();
  });
});
