import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';

vi.mock('@tiptap/react', () => {
  const NodeViewWrapper = ({ children, ...rest }: any) => (
    <div data-testid="nvw" {...rest}>
      {children}
    </div>
  );
  const NodeViewContent = ({ as = 'div', ...rest }: any) => {
    const Tag = as;
    return <Tag data-testid="nvc" {...rest} />;
  };
  return { NodeViewWrapper, NodeViewContent };
});

vi.mock('../mermaid-diagram', () => ({
  MermaidDiagram: () => null,
}));

vi.mock('../../i18n', () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        code_block: {
          copy_code: 'Copy code',
          show_preview: 'Show preview',
          show_source: 'Show source',
        },
      }),
  }),
}));

import { CodeBlockView } from './code-block-view';

function makeProps(language: string, text: string) {
  return {
    node: {
      attrs: { language },
      textContent: text,
    },
  } as unknown as Parameters<typeof CodeBlockView>[0];
}

describe('CodeBlockView — html language toggle', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("defaults to preview view: renders an iframe with sandbox='allow-scripts' and keeps the <pre> mounted (hidden)", () => {
    render(<CodeBlockView {...makeProps('html', '<p>hello</p>')} />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    const frame = document.querySelector('iframe');
    expect(frame).toBeTruthy();
    expect(frame?.getAttribute('sandbox')).toBe('allow-scripts');
    const nvc = screen.getByTestId('nvc');
    expect(nvc).toBeTruthy();
    const pre = nvc.closest('pre');
    expect(pre).toBeTruthy();
    expect(pre?.className).toContain('sr-only');
  });

  it('toggles to source view: iframe is removed and the <pre> is no longer hidden', () => {
    render(<CodeBlockView {...makeProps('html', '<p>hello</p>')} />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(document.querySelector('iframe')).toBeTruthy();
    const toggle = screen.getByTitle('Show source');
    fireEvent.click(toggle);
    expect(document.querySelector('iframe')).toBeNull();
    const nvc = screen.getByTestId('nvc');
    const pre = nvc.closest('pre')!;
    expect(pre.className).not.toContain('sr-only');
  });

  it('does not show the toggle or an iframe for a non-html language', () => {
    render(<CodeBlockView {...makeProps('typescript', 'const x = 1;')} />);
    expect(screen.queryByTitle('Show source')).toBeNull();
    expect(screen.queryByTitle('Show preview')).toBeNull();
    expect(document.querySelector('iframe')).toBeNull();
  });
});
