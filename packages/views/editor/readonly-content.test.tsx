import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { readFileSync } from 'node:fs';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const { getAttachmentTextContentMock, resolveIssueIdentifierMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
  resolveIssueIdentifierMock: vi.fn(),
}));

vi.mock('../issues/hooks', () => ({
  useResolveIssueIdentifier: (identifier: string) => resolveIssueIdentifierMock(identifier),
}));

vi.mock('../i18n', async () => {
  const editor = (await import('../locales/en/editor.json')).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
    useTimeAgo: () => 'just now',
  };
});

vi.mock('@goosar/core/api', () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
  useWorkspaceSlug: () => 'test',
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useAppOrigin: () => null,
}));

vi.mock('../issues/components/issue-mention-card', () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-mention-card">{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock('./extensions/image-view', () => ({
  ImageLightbox: () => null,
}));

vi.mock('./link-hover-card', () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock('./utils/link-handler', () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith('mention://')),
}));

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn().mockResolvedValue({
      svg: '<svg viewBox="0 0 123 45"><g><text>mock diagram</text></g></svg>',
    }),
  },
}));

Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  value: () => ({
    fillStyle: '#000',
    fillRect: vi.fn(),
    getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
  }),
});

import mermaid from 'mermaid';
import { ReadonlyContent } from './readonly-content';

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ReadonlyContent memoization', () => {
  it('is wrapped in React.memo', () => {
    const memoTypeSymbol = Symbol.for('react.memo');
    expect((ReadonlyContent as unknown as { $$typeof: symbol }).$$typeof).toBe(memoTypeSymbol);
  });
});

describe('ReadonlyContent math rendering', () => {
  it('renders inline and block LaTeX with KaTeX markup', () => {
    const { container } = render(
      <ReadonlyContent
        content={['Inline math: $$E = mc^2$$', '', '$$', '\\int_0^1 x^2 \\, dx', '$$'].join('\n')}
      />,
    );

    const text = container.textContent?.replace(/\s+/g, ' ') ?? '';
    expect(container.querySelectorAll('.katex').length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector('.katex-display')).not.toBeNull();
    expect(text).toContain('E = mc^2');
    expect(text).toContain('\\int_0^1 x^2 \\, dx');
  });
});

describe('ReadonlyContent line breaks', () => {
  it('converts a single newline into a <br>', () => {
    const { container } = render(<ReadonlyContent content={'line one\nline two'} />);
    expect(container.querySelector('br')).not.toBeNull();
  });

  it('renders a blank-line gap as separate paragraphs', () => {
    const { container } = render(<ReadonlyContent content={'para one\n\npara two'} />);
    expect(container.querySelectorAll('p').length).toBeGreaterThanOrEqual(2);
  });
});

describe('ReadonlyContent autolink policy', () => {
  it('keeps historical bare filenames and domains as plain text', () => {
    const { container } = render(<ReadonlyContent content="plan.md 4399.com ai.md" />);

    expect(container.textContent).toContain('plan.md 4399.com ai.md');
    expect(container.querySelector('a')).toBeNull();
  });

  it('still links explicit web URLs, www URLs, and email addresses', () => {
    const { container } = render(
      <ReadonlyContent content="https://4399.com www.4399.com contact@example.com" />,
    );

    const hrefs = Array.from(container.querySelectorAll('a'), (anchor) =>
      anchor.getAttribute('href'),
    );
    expect(hrefs).toEqual([
      'https://4399.com',
      'https://www.4399.com',
      'mailto:contact@example.com',
    ]);
  });
});

describe('ReadonlyContent task lists', () => {
  it('renders `- [ ]` / `- [x]` as checkboxes and preserves the checked state', () => {
    const { container } = render(<ReadonlyContent content={'- [ ] todo\n- [x] done'} />);

    const boxes = container.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');
    expect(boxes).toHaveLength(2);
    expect(boxes[0]!.checked).toBe(false);
    expect(boxes[1]!.checked).toBe(true);
    expect(boxes[1]!.disabled).toBe(true);
  });

  it('nests a child task list inside its parent item (not as a sibling)', () => {
    const { container } = render(
      <ReadonlyContent content={'- [ ] parent\n  - [x] child\n  - [ ] child2'} />,
    );

    const root = container.querySelector('ul.contains-task-list');
    expect(root).not.toBeNull();
    const topItems = root!.querySelectorAll(':scope > li.task-list-item');
    expect(topItems).toHaveLength(1);

    const parent = topItems[0]!;
    const nested = parent.querySelector(':scope > ul.contains-task-list');
    expect(nested).not.toBeNull();

    const childItems = nested!.querySelectorAll(':scope > li.task-list-item');
    expect(childItems).toHaveLength(2);
    const childBoxes = nested!.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');
    expect(childBoxes[0]!.checked).toBe(true);
    expect(childBoxes[1]!.checked).toBe(false);
  });
});

describe('ReadonlyContent highlight Markdown', () => {
  it('renders ==text== as a <mark> element', () => {
    const { container } = render(<ReadonlyContent content={'a ==hi== b'} />);
    const mark = container.querySelector('mark');
    expect(mark).not.toBeNull();
    expect(mark?.textContent).toBe('hi');
  });

  it('keeps inner Markdown formatting inside a highlight', () => {
    const { container } = render(<ReadonlyContent content={'==**bold**=='} />);
    expect(container.querySelector('mark strong')).not.toBeNull();
  });

  it('does not highlight == inside inline code', () => {
    const { container } = render(<ReadonlyContent content={'`a ==b== c`'} />);
    expect(container.querySelector('mark')).toBeNull();
    expect(container.querySelector('code')?.textContent).toBe('a ==b== c');
  });

  it('wraps the whole span when an inner == lives in inline code', () => {
    const { container } = render(<ReadonlyContent content={'==a `b==c` d=='} />);
    const mark = container.querySelector('mark');
    expect(mark).not.toBeNull();
    expect(mark?.querySelector('code')?.textContent).toBe('b==c');
    expect(mark?.textContent).toBe('a b==c d');
  });

  it('does not highlight across a blank line', () => {
    const { container } = render(<ReadonlyContent content={'==a\n\nb=='} />);
    expect(container.querySelector('mark')).toBeNull();
  });
});

describe('ReadonlyContent issue mention Markdown', () => {
  it('renders an issue mention inside a task list as an issue mention card', () => {
    const { container, getByTestId } = render(
      <ReadonlyContent content="- [ ] [MUL-123](mention://issue/issue-123)" />,
    );

    expect(container.querySelector('input[type="checkbox"]')).not.toBeNull();
    expect(getByTestId('issue-mention-card').textContent).toBe('MUL-123');
  });

  it('autolinks a resolved bare identifier as an issue mention card', () => {
    resolveIssueIdentifierMock.mockImplementation((id: string) =>
      id === 'MUL-7' ? { id: 'issue-7', identifier: 'MUL-7' } : null,
    );

    const { getByTestId } = render(<ReadonlyContent content="See MUL-7 for context" />);

    expect(getByTestId('issue-mention-card').textContent).toBe('MUL-7');
    expect(resolveIssueIdentifierMock).toHaveBeenCalledWith('MUL-7');
  });

  it('leaves an unresolved bare identifier as plain text', () => {
    resolveIssueIdentifierMock.mockReturnValue(null);

    const { container, queryByTestId } = render(
      <ReadonlyContent content="See MUL-999 for context" />,
    );

    expect(queryByTestId('issue-mention-card')).toBeNull();
    expect(container.textContent).toContain('MUL-999');
  });

  it('does not autolink a bare identifier inside inline code', () => {
    resolveIssueIdentifierMock.mockReturnValue(null);

    const { queryByTestId } = render(<ReadonlyContent content={'use `MUL-7` here'} />);

    expect(resolveIssueIdentifierMock).not.toHaveBeenCalled();
    expect(queryByTestId('issue-mention-card')).toBeNull();
  });

  it('documents the CommonMark quoted-emphasis edge case before Korean particles', () => {
    const unsafe = render(<ReadonlyContent content={'**"무엇을 먼저 정해두고 시작할지"**가'} />);

    expect(unsafe.container.querySelector('strong')).toBeNull();
    expect(unsafe.container.textContent).toContain('**"무엇을 먼저 정해두고 시작할지"**가');

    const safe = render(<ReadonlyContent content={'"**무엇을 먼저 정해두고 시작할지**"가'} />);

    expect(safe.container.querySelector('strong')?.textContent).toBe(
      '무엇을 먼저 정해두고 시작할지',
    );
    expect(safe.container.textContent).toContain('"무엇을 먼저 정해두고 시작할지"가');
  });
});

describe('ReadonlyContent code styling', () => {
  const literalCode = 'uv run --extra dev pytest -q';

  it('renders inline and fenced code through rich-text-editor code selectors', () => {
    const { container } = render(
      <ReadonlyContent
        content={[`<code>${literalCode}</code>`, '', '```', literalCode, '```'].join('\n')}
      />,
    );

    const inlineCode = Array.from(container.querySelectorAll('code')).find(
      (code) => !code.closest('pre'),
    );
    const blockCode = container.querySelector('pre code');

    expect(inlineCode?.textContent).toBe(literalCode);
    expect(blockCode?.textContent).toBe(literalCode);
  });

  it('renders code blocks without a language tag as plaintext', () => {
    const token = 'const answer = 42;';
    const { container } = render(<ReadonlyContent content={['```', token, '```'].join('\n')} />);
    const blockCode = container.querySelector('pre code');
    expect(blockCode?.textContent?.trim()).toBe(token);
    expect(blockCode?.querySelector('span')).toBeNull();
  });

  it('copies the whole fenced code block from the readonly toolbar', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
    const source = ['pnpm install', 'pnpm test'].join('\n');
    const { getByRole } = render(
      <ReadonlyContent content={['```bash', source, '```'].join('\n')} />,
    );

    fireEvent.click(getByRole('button', { name: 'Copy code' }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(source);
    });
  });

  it('keeps editor code literal by disabling font ligatures', () => {
    const codeCss = readFileSync('editor/styles/code.css', 'utf8');

    expect(codeCss).toContain('.rich-text-editor code');
    expect(codeCss).toContain('.rich-text-editor pre');
    expect(codeCss).toContain('.rich-text-editor pre code');
    expect(codeCss).toContain('font-variant-ligatures: none;');
    expect(codeCss).toContain('font-feature-settings: "liga" 0;');
  });
});

describe('ReadonlyContent Mermaid rendering', () => {
  it('renders mermaid code fences in a sized sandbox iframe with legacy rgb colors', async () => {
    const originalGetComputedStyle = window.getComputedStyle;
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element, pseudoElt) => {
      if (element instanceof HTMLElement && element.style.color.startsWith('var(')) {
        return { color: 'oklch(60% 0.2 120)' } as CSSStyleDeclaration;
      }
      return originalGetComputedStyle.call(window, element, pseudoElt);
    });

    const { container } = render(
      <ReadonlyContent
        content={['```mermaid', 'graph LR', '  A[Start] --> B[Done]', '```'].join('\n')}
      />,
    );

    expect(container.querySelector('.mermaid-diagram')).not.toBeNull();
    expect(container.querySelector('pre code.language-mermaid')).toBeNull();

    await waitFor(() => {
      const iframe = container.querySelector<HTMLIFrameElement>('.mermaid-diagram-frame');
      expect(iframe).not.toBeNull();
      expect(iframe?.getAttribute('sandbox')).toBe('');
      expect(iframe?.srcdoc).toContain('mock diagram');
      expect(iframe?.style.width).toBe('123px');
      expect(iframe?.style.height).toBe('45px');
    });

    expect(mermaid.initialize).toHaveBeenCalledWith(
      expect.objectContaining({
        themeVariables: expect.objectContaining({
          lineColor: 'rgb(12, 34, 56)',
          primaryBorderColor: 'rgb(12, 34, 56)',
          primaryColor: 'rgb(12, 34, 56)',
          primaryTextColor: 'rgb(12, 34, 56)',
        }),
      }),
    );
  });

  it('does not regress Mermaid unwrap after the HtmlBlockPreview branch was added', async () => {
    const { container } = render(
      <ReadonlyContent content={['```mermaid', 'graph LR', '  A --> B', '```'].join('\n')} />,
    );
    expect(container.querySelector('.mermaid-diagram')).not.toBeNull();
    expect(container.querySelector('pre')).toBeNull();
  });

  it('opens the fullscreen viewer from the toolbar and closes it with Escape', async () => {
    const { container } = render(
      <ReadonlyContent
        content={['```mermaid', 'graph LR', '  A[Start] --> B[Done]', '```'].join('\n')}
      />,
    );

    const expandButton = await waitFor(() => {
      const found = container.querySelector<HTMLButtonElement>(
        '.mermaid-diagram-toolbar button[aria-label="Open diagram viewer"]',
      );
      expect(found).not.toBeNull();
      return found!;
    });

    expect(document.querySelector('.zoom-canvas')).toBeNull();

    fireEvent.click(expandButton);

    const viewerFrame = await waitFor(() => {
      const found = document.querySelector<HTMLIFrameElement>('.mermaid-viewer-frame');
      expect(found).not.toBeNull();
      return found!;
    });
    expect(viewerFrame.getAttribute('sandbox')).toBe('');
    expect(viewerFrame.srcdoc).toContain('mock diagram');
    expect(viewerFrame.srcdoc).toContain('width: 123px');
    expect(viewerFrame.srcdoc).toContain('max-width: none');

    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(document.querySelector('.zoom-canvas')).toBeNull();
    });
  });

  it('keeps the inline toolbar outside the scroll container so wide diagrams stay openable', async () => {
    const { container } = render(
      <ReadonlyContent
        content={['```mermaid', 'graph LR', '  A[Start] --> B[Done]', '```'].join('\n')}
      />,
    );

    await waitFor(() => {
      expect(container.querySelector('.mermaid-diagram-toolbar')).not.toBeNull();
    });

    const scroller = container.querySelector('.mermaid-diagram-scroll');
    expect(scroller).not.toBeNull();
    expect(scroller?.querySelector('.mermaid-diagram-toolbar')).toBeNull();
  });

  it("shows the compact error state instead of embedding Mermaid's parser error SVG", async () => {
    vi.mocked(mermaid.render).mockRejectedValueOnce(new Error('Parse error on line 3'));

    const chart = 'graph LR\n  A -->';
    const { container } = render(
      <ReadonlyContent content={['```mermaid', chart, '```'].join('\n')} />,
    );

    await waitFor(() => {
      expect(container.querySelector('.mermaid-diagram-error')).not.toBeNull();
    });

    expect(container.querySelector('.mermaid-diagram-frame')).toBeNull();
    expect(container.querySelector('.mermaid-diagram-error code')?.textContent).toBe(chart);
  });
});

describe('ReadonlyContent HTML block rendering', () => {
  it("renders an iframe with sandbox='allow-scripts' for ```html and skips the outer <pre>", () => {
    const { container } = render(
      <ReadonlyContent content={['```html', '<h1 id="x">hi</h1>', '```'].join('\n')} />,
    );
    const frame = container.querySelector<HTMLIFrameElement>('iframe');
    expect(frame).not.toBeNull();
    expect(frame?.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame?.getAttribute('srcdoc')).toContain('<h1 id="x">hi</h1>');
    expect(container.querySelector('pre')).toBeNull();
  });

  it('keeps the <pre><code> wrapper for adjacent languages like htmlbars / mermaidx', () => {
    const { container } = render(
      <ReadonlyContent
        content={[
          '```htmlbars',
          '<div>{{name}}</div>',
          '```',
          '',
          '```mermaidx',
          'not a real lang',
          '```',
        ].join('\n')}
      />,
    );
    const pres = container.querySelectorAll('pre');
    expect(pres.length).toBe(2);
    expect(container.querySelector('pre code.language-htmlbars')).not.toBeNull();
    expect(container.querySelector('pre code.language-mermaidx')).not.toBeNull();
  });
});

describe('ReadonlyContent file-card → AttachmentBlock HTML routing', () => {
  function renderWithQuery(ui: ReactElement) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  }

  it('renders the !file[](url) HTML attachment as an iframe (no file-card chrome)', async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: '<p>chart</p>',
      originalContentType: 'text/html',
    });
    const attachment = {
      id: 'att-1',
      url: '/uploads/report.html',
      filename: 'report.html',
      content_type: 'text/html',
      size_bytes: 0,
    } as any;
    const { container, queryByText } = renderWithQuery(
      <ReadonlyContent
        content="!file[report.html](/uploads/report.html)"
        attachments={[attachment]}
      />,
    );
    const frame = await waitFor(() => {
      const f = container.querySelector<HTMLIFrameElement>('iframe');
      expect(f).not.toBeNull();
      return f!;
    });
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('srcdoc')).toContain('<p>chart</p>');
    expect(queryByText('report.html')).toBeNull();
  });

  it('renders a stable attachment download URL as file-card chrome', () => {
    const id = '11111111-2222-3333-4444-555555555555';
    const href = `/api/attachments/${id}/download`;
    const attachment = {
      id,
      url: '/uploads/report.pdf',
      filename: 'report.pdf',
      content_type: 'application/pdf',
      size_bytes: 1024,
      markdown_url: href,
      download_url: href,
    } as any;

    const { container, getByText } = renderWithQuery(
      <ReadonlyContent content={`!file[report.pdf](${href})`} attachments={[attachment]} />,
    );

    expect(getByText('report.pdf')).toBeTruthy();
    expect(container.querySelector('iframe')).toBeNull();
    expect(container.querySelector('img')).toBeNull();
  });

  it('resolves a markdown image whose src is the response download_url', () => {
    const href = 'https://cdn.example.test/shot.png?Signature=stale';
    const fresh = 'https://cdn.example.test/shot.png?Signature=fresh';
    const attachment = {
      id: '11111111-2222-3333-4444-555555555555',
      url: 'https://cdn.example.test/shot.png',
      download_url: fresh,
      markdown_url: '/api/attachments/11111111-2222-3333-4444-555555555555/download',
      filename: 'shot.png',
      content_type: 'image/png',
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <ReadonlyContent content={`![](${href})`} attachments={[attachment]} />,
    );

    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe(fresh);
    expect(img?.getAttribute('alt')).toBe('shot.png');
  });
});

describe('ReadonlyContent inline data-URI images', () => {
  const PNG_1X1 =
    'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

  function renderWithQuery(ui: ReactElement) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  }

  it('preserves the src of an inline data:image/png image', () => {
    const { container } = renderWithQuery(<ReadonlyContent content={`![QR Code](${PNG_1X1})`} />);

    expect(container.querySelector('img')?.getAttribute('src')).toBe(PNG_1X1);
  });

  it('strips non-image data URIs (data:text/html)', () => {
    const { container } = renderWithQuery(
      <ReadonlyContent content={'![x](data:text/html,<script>alert(1)</script>)'} />,
    );

    expect(container.querySelector('img')?.getAttribute('src') ?? '').toBe('');
  });
});

describe('ReadonlyContent slash command rendering', () => {
  it('renders slash skill links as slash command pills', () => {
    const { container } = render(<ReadonlyContent content="[/deploy](slash://skill/abc-123)" />);

    const pill = container.querySelector('.slash-command');
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe('/deploy');
  });

  it('does not affect regular links', () => {
    const { container } = render(<ReadonlyContent content="[docs](https://example.com)" />);

    expect(container.querySelector('.slash-command')).toBeNull();
    expect(container.querySelector('a')).not.toBeNull();
  });
});

describe('ReadonlyContent bare URL autolinking (MUL-4242)', () => {
  it('renders a bold-wrapped bare URL as bold plus a clean link', () => {
    const url = 'https://github.com/adanman/goosar/pull/5081';
    const { container } = render(<ReadonlyContent content={`**PR：${url}**`} />);

    const strong = container.querySelector('strong');
    expect(strong).not.toBeNull();
    const anchor = strong!.querySelector('a');
    expect(anchor?.getAttribute('href')).toBe(url);
    expect(container.textContent).not.toContain('**');
    expect(anchor?.getAttribute('href')).not.toContain('*');
  });

  it('bolds a bare URL even when a CJK punctuation immediately follows (variant B)', () => {
    const url = 'https://github.com/adanman/goosar/pull/5133';
    const { container } = render(<ReadonlyContent content={`PR：**${url}**（MUL-4277）。`} />);

    const strong = container.querySelector('strong');
    expect(strong).not.toBeNull();
    expect(strong!.querySelector('a')?.getAttribute('href')).toBe(url);
    expect(container.textContent).not.toContain('**');
    expect(container.textContent).toContain('（MUL-4277）');
  });

  it('still autolinks a plain bare URL', () => {
    const { container } = render(<ReadonlyContent content={'see https://example.com/foo here'} />);
    expect(container.querySelector('a[href="https://example.com/foo"]')).not.toBeNull();
  });

  it('stops an autolinked URL at CJK punctuation instead of swallowing it', () => {
    const { container } = render(
      <ReadonlyContent content={'见 https://example.com/foo。后面还有字'} />,
    );
    const anchor = container.querySelector('a');
    expect(anchor?.getAttribute('href')).toBe('https://example.com/foo');
    expect(anchor?.textContent).toBe('https://example.com/foo');
    expect(container.textContent).toContain('。后面还有字');
  });

  it('keeps every URL in a CJK-separated run linked, not just the first', () => {
    const { container } = render(
      <ReadonlyContent content={'两个地址 https://a.com/x、https://b.com/y'} />,
    );
    const hrefs = Array.from(container.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('https://a.com/x');
    expect(hrefs).toContain('https://b.com/y');
    expect(container.textContent).toContain('、');
  });

  it("leaves an explicit link's destination untouched even when it ends in CJK", () => {
    const { container } = render(<ReadonlyContent content={'[看](https://example.com/x。)后文'} />);
    const anchor = container.querySelector('a');
    expect(decodeURIComponent(anchor?.getAttribute('href') ?? '')).toBe('https://example.com/x。');
    expect(anchor?.textContent).toBe('看');
  });
});
