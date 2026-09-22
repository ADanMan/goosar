// Сопоставление позиции курсора между readonly-заглушкой и редактором
// через текстовые якоря, а не пиксельные координаты.

import type { Node as PMNode } from '@tiptap/pm/model';

export interface TextAnchor {
  block: number;
  offset: number;
  bias: 1 | -1;
}

const NON_WS = /\S/;

function countNonWs(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) if (NON_WS.test(s.charAt(i))) n++;
  return n;
}

export function anchorFromPoint(x: number, y: number, root: HTMLElement): TextAnchor | null {
  const doc = root.ownerDocument;
  let node: Node | null = null;
  let nodeOffset = 0;
  if (typeof doc.caretPositionFromPoint === 'function') {
    const p = doc.caretPositionFromPoint(x, y);
    if (p) {
      node = p.offsetNode;
      nodeOffset = p.offset;
    }
  } else if (typeof doc.caretRangeFromPoint === 'function') {
    const r = doc.caretRangeFromPoint(x, y);
    if (r) {
      node = r.startContainer;
      nodeOffset = r.startOffset;
    }
  }
  if (!node || !root.contains(node) || node === root) return null;

  let blockEl: Node = node;
  while (blockEl.parentNode && blockEl.parentNode !== root) {
    blockEl = blockEl.parentNode;
  }
  if (blockEl.parentNode !== root || blockEl.nodeType !== Node.ELEMENT_NODE) {
    return null;
  }
  const block = Array.prototype.indexOf.call(root.children, blockEl);
  if (block < 0) return null;

  const range = doc.createRange();
  try {
    range.setStart(blockEl, 0);
    range.setEnd(node, nodeOffset);
  } catch {
    return { block, offset: 0, bias: 1 };
  }
  const before = range.toString();
  const nextChar = (blockEl.textContent ?? '').charAt(before.length);
  return {
    block,
    offset: countNonWs(before),
    bias: NON_WS.test(nextChar) ? 1 : -1,
  };
}

export function posFromAnchor(doc: PMNode, anchor: TextAnchor): number {
  if (doc.childCount === 0) return 0;
  if (anchor.block >= doc.childCount) return doc.content.size;
  const blockIndex = Math.max(0, anchor.block);

  let blockStart = 0;
  for (let i = 0; i < blockIndex; i++) blockStart += doc.child(i).nodeSize;
  const block = doc.child(blockIndex);
  const contentStart = blockStart + 1;

  const wanted = Math.max(0, anchor.offset);
  let seen = 0;
  let afterPrev = contentStart;
  let resolved: number | null = null;
  block.descendants((child, pos) => {
    if (resolved !== null) return false;
    if (!child.isText) return true;
    const text = child.text ?? '';
    for (let i = 0; i < text.length; i++) {
      if (!NON_WS.test(text.charAt(i))) continue;
      if (seen === wanted) {
        resolved = anchor.bias === 1 ? contentStart + pos + i : afterPrev;
        return false;
      }
      seen++;
      afterPrev = contentStart + pos + i + 1;
    }
    return true;
  });
  if (resolved !== null) return resolved;
  return seen === wanted ? afterPrev : blockStart + block.nodeSize - 1;
}
