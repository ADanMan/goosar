import { fromMarkdown } from 'mdast-util-from-markdown';
import type { Root, RootContent, Code } from 'mdast';

const FENCE_OPEN_RE = /^ {0,3}(`{3,}|~{3,})/;

function isCodeNode(node: RootContent | Root): node is Code {
  return node.type === 'code';
}

function collectCodeNodes(node: Root | RootContent, out: Code[]): void {
  if (isCodeNode(node)) {
    out.push(node);
    return;
  }
  const children = (node as { children?: RootContent[] }).children;
  if (children) {
    for (const child of children) collectCodeNodes(child, out);
  }
}

function endsWithClosingFence(raw: string): boolean {
  const lines = raw.split('\n');
  const openMatch = FENCE_OPEN_RE.exec(lines[0] ?? '');
  if (!openMatch?.[1]) return true;

  const openFence = openMatch[1];
  const marker = openFence[0] as '`' | '~';
  if (lines.length < 2) return false;

  const lastLine =
    (lines[lines.length - 1] === '' ? lines[lines.length - 2] : lines[lines.length - 1]) ?? '';
  const closeMatch = /^ {0,3}([`~]+)[ \t]*$/.exec(lastLine);
  if (!closeMatch?.[1]) return false;

  const closeFence = closeMatch[1];
  return closeFence[0] === marker && closeFence.length >= openFence.length;
}

export function computeClosedFenceOffsets(source: string): Set<number> {
  const closed = new Set<number>();
  if (!source) return closed;

  let tree: Root;
  try {
    tree = fromMarkdown(source);
  } catch {
    return closed;
  }

  const codes: Code[] = [];
  collectCodeNodes(tree, codes);

  for (const node of codes) {
    const start = node.position?.start.offset;
    const end = node.position?.end.offset;
    if (start == null || end == null) continue;
    if (endsWithClosingFence(source.slice(start, end))) closed.add(start);
  }

  return closed;
}
