import { describe, expect, it } from 'vitest';
import { computeClosedFenceOffsets } from './streaming-fence';

const isClosed = (src: string, offset = 0): boolean => computeClosedFenceOffsets(src).has(offset);

describe('computeClosedFenceOffsets', () => {
  it('treats a closed backtick fence as closed', () => {
    expect(isClosed('```mermaid\ngraph TD\n```\n')).toBe(true);
  });

  it('treats an unterminated fence as open', () => {
    expect(isClosed('```mermaid\ngraph TD\n')).toBe(false);
  });

  it('treats a bare opener with no content as open', () => {
    expect(isClosed('```mermaid\n')).toBe(false);
  });

  it('stays open while content accumulates', () => {
    expect(isClosed('```mermaid\nflowchart LR\n  A --> B\n  B --> C\n')).toBe(false);
  });

  it('handles tilde fences', () => {
    expect(isClosed('~~~mermaid\ngraph TD\n~~~\n')).toBe(true);
    expect(isClosed('~~~mermaid\ngraph TD\n')).toBe(false);
  });

  it('does not accept a tilde closer for a backtick opener', () => {
    expect(isClosed('```mermaid\ngraph TD\n~~~\n')).toBe(false);
  });

  it('does not accept a shorter closer than the opener', () => {
    expect(isClosed('````mermaid\ngraph TD\n```\n')).toBe(false);
  });

  it('accepts a longer closer than the opener', () => {
    expect(isClosed('```mermaid\ngraph TD\n`````\n')).toBe(true);
  });

  it('handles an indented fence', () => {
    expect(isClosed('  ```mermaid\n  graph TD\n  ```\n', 2)).toBe(true);
  });

  it('handles a fence inside a list item', () => {
    const src = '- item\n\n  ```mermaid\n  graph TD\n  ```\n';
    expect(computeClosedFenceOffsets(src).size).toBe(1);
    expect(isClosed(src, src.indexOf('```'))).toBe(true);
  });

  it('does not report an inner fence of a nested block', () => {
    const src = '````md\n```mermaid\ngraph TD\n```\n````\n';
    const offsets = computeClosedFenceOffsets(src);
    expect(offsets.size).toBe(1);
    expect(offsets.has(0)).toBe(true);
  });

  it('reports each block independently when several are present', () => {
    const src = '```mermaid\ngraph TD\n```\n\ntext\n\n```html\n<b>x</b>\n';
    const offsets = computeClosedFenceOffsets(src);
    expect(offsets.has(0)).toBe(true);
    expect(offsets.has(src.lastIndexOf('```html'))).toBe(false);
  });

  it('flips to closed exactly when the closing fence arrives', () => {
    const steps = [
      '```mermaid',
      '```mermaid\n',
      '```mermaid\nflowchart LR\n',
      '```mermaid\nflowchart LR\n  A --> B\n',
      '```mermaid\nflowchart LR\n  A --> B\n``',
      '```mermaid\nflowchart LR\n  A --> B\n```',
    ];
    const results = steps.map((s) => isClosed(s));
    expect(results).toEqual([false, false, false, false, false, true]);
  });

  it('ignores an indented (non-fenced) code block', () => {
    const src = '    const a = 1\n';
    expect(computeClosedFenceOffsets(src).size).toBe(1);
  });

  it('returns an empty set for empty input', () => {
    expect(computeClosedFenceOffsets('').size).toBe(0);
  });

  it('returns an empty set for prose with no code', () => {
    expect(computeClosedFenceOffsets('just some **text**\n').size).toBe(0);
  });
});
