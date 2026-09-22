/**
 * Политика внешних изображений в markdown.
 *
 * URL внешнего изображения, указанный в markdown, заставляет клиента
 * обращаться к произвольному стороннему серверу — канал утечки данных,
 * контролируемый контентом. Деплой решает через `GOOSAR_EXTERNAL_IMAGES`
 * (доставляется через /api/config → configStore → useSyncMarkdownImagePolicy
 * → сеттер модуля packages/ui), рендерить ли внешние изображения (`allow`,
 * по умолчанию), блокировать их (`block`) или ограничивать списком
 * `GOOSAR_IMAGE_HOSTS` (`allowlist`).
 *
 * Тесты прогоняют фикстуры через обе продуктовые цепочки markdown — базовую
 * (`@goosar/ui/markdown` Markdown) и продуктовый рендерер RichContent,
 * используемый в описаниях issue, комментариях и сообщениях чата.
 *
 * Инварианты вложений, проверяемые здесь (должны выполняться в ЛЮБОМ режиме):
 *   1. сохранённая относительная ссылка `/api/attachments/<uuid>/download`
 *      доходит до слоя <Attachment> без изменений — подмена на подписанный
 *      абсолютный URL происходит внутри <Attachment> уже после проверки
 *      политики;
 *   2. старый абсолютный `att.url` на CDN-домене деплоя по-прежнему
 *      рендерится, так как мост добавляет CDN-домен в список всегда
 *      доверенных хостов;
 *   3. относительные ссылки `/uploads/...` и встроенные `data:image/*`
 *      рендерятся в любом режиме.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render } from '@testing-library/react';
import type { Attachment } from '@goosar/core/types';
import {
  Markdown as MarkdownBase,
  markdownUrlTransform,
  setMarkdownImagePolicy,
} from '@goosar/ui/markdown';
import { resetMarkdownImagePolicy } from '@goosar/ui/markdown/image-policy';

const mockConfig: {
  cdnDomain: string;
  externalImages: 'allow' | 'block' | 'allowlist';
  imageHosts: string[];
} = {
  cdnDomain: '',
  externalImages: 'allow',
  imageHosts: [],
};

vi.mock('@goosar/core/config', () => ({
  useConfigStore: (selector: (state: typeof mockConfig) => unknown) => selector(mockConfig),
  configStore: { getState: () => mockConfig },
}));

vi.mock('../issues/hooks', () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock('../i18n', async () => {
  const editor = (await import('../locales/en/editor.json')).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => 'just now',
  };
});

vi.mock('@goosar/core/api', () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock('@goosar/core/paths', () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
  useWorkspaceSlug: () => 'test',
}));

vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useAppOrigin: () => null,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock('../issues/components/issue-mention-card', () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));

vi.mock('../projects/components/project-chip', () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => <span>{projectId}</span>,
}));

vi.mock('../editor/link-hover-card', () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock('../editor/utils/link-handler', () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith('mention://')),
}));

vi.mock('../editor/attachment', () => ({
  Attachment: ({ attachment }: { attachment: { url: string; filename: string } }) => (
    <img data-attachment-layer="" src={attachment.url} alt={attachment.filename} />
  ),
}));

import { RichContent } from './rich-content';

const ATTACHMENT_REF = '/api/attachments/0195c9a1-1111-7bbb-8ccc-abcdefabcdef/download';
const LEGACY_CDN_URL = 'https://goosar-static.example.com/uploads/ws1/old-file.png';
const PNG_1X1 =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

function setPolicy(
  mode: 'allow' | 'block' | 'allowlist',
  hosts: string[] = [],
  cdnDomain = '',
): void {
  mockConfig.externalImages = mode;
  mockConfig.imageHosts = hosts;
  mockConfig.cdnDomain = cdnDomain;
  setMarkdownImagePolicy({ mode, hosts: [...hosts, cdnDomain] });
}

afterEach(() => {
  cleanup();
  mockConfig.externalImages = 'allow';
  mockConfig.imageHosts = [];
  mockConfig.cdnDomain = '';
  resetMarkdownImagePolicy();
});

const SURFACES: ReadonlyArray<{
  name: string;
  render: (markdown: string) => HTMLElement;
}> = [
  {
    name: 'Chat/base (ui/markdown)',
    render: (markdown) => render(<MarkdownBase>{markdown}</MarkdownBase>).container,
  },
  {
    name: 'RichContent (issue/comment/chat message)',
    render: (markdown) => render(<RichContent content={markdown} />).container,
  },
];

describe.each(SURFACES)('external-image policy — $name', ({ render: renderSurface }) => {
  it('allow (default): external http(s) image renders as before', () => {
    setPolicy('allow');
    const container = renderSurface('![cat](https://cdn.example.com/cat.png)');

    expect(container.querySelector('img')).toHaveAttribute(
      'src',
      'https://cdn.example.com/cat.png',
    );
    expect(container.querySelector('[data-blocked-image]')).toBeNull();
  });

  it('block: external image becomes an inert placeholder, no fetchable src', () => {
    setPolicy('block');
    const container = renderSurface('![leak](https://attacker.example/x.png)');

    for (const img of Array.from(container.querySelectorAll('img'))) {
      expect(img.getAttribute('src') ?? '').toBe('');
    }
    const placeholder = container.querySelector('[data-blocked-image]');
    expect(placeholder).not.toBeNull();
    expect(placeholder?.textContent).toContain('leak');
  });

  it('block: raw HTML <img> is gated the same as markdown syntax', () => {
    setPolicy('block');
    const container = renderSurface('<img src="https://attacker.example/x.png" alt="raw">');

    for (const img of Array.from(container.querySelectorAll('img'))) {
      expect(img.getAttribute('src') ?? '').toBe('');
    }
    expect(container.querySelector('[data-blocked-image]')).not.toBeNull();
  });

  it('allowlist: listed host renders, unlisted host is blocked', () => {
    setPolicy('allowlist', ['cdn.example.com']);
    const container = renderSurface(
      '![ok](https://cdn.example.com/ok.png)\n\n![bad](https://attacker.example/x.png)',
    );

    const srcs = Array.from(container.querySelectorAll('img')).map(
      (img) => img.getAttribute('src') ?? '',
    );
    expect(srcs).toContain('https://cdn.example.com/ok.png');
    expect(srcs).not.toContain('https://attacker.example/x.png');
    expect(container.querySelector('[data-blocked-image]')).not.toBeNull();
  });

  it('block: protocol-relative and backslash-disguised URLs are blocked', () => {
    setPolicy('block');
    const container = renderSurface(
      '<img src="//attacker.example/x.png" alt="a"><img src="/\\attacker.example/y.png" alt="b">',
    );

    for (const img of Array.from(container.querySelectorAll('img'))) {
      expect(img.getAttribute('src') ?? '').toBe('');
    }
  });

  it('block: site-relative attachment ref reaches the attachment layer untouched', () => {
    setPolicy('block');
    const container = renderSurface(`![screenshot](${ATTACHMENT_REF})`);

    expect(container.querySelector('img')).toHaveAttribute('src', ATTACHMENT_REF);
    expect(container.querySelector('[data-blocked-image]')).toBeNull();
  });

  it('block: site-relative /uploads path still renders', () => {
    setPolicy('block');
    const container = renderSurface('![local](/uploads/ws1/pic.png)');

    expect(container.querySelector('img')).toHaveAttribute('src', '/uploads/ws1/pic.png');
  });

  it('block: inline data:image URI still renders', () => {
    setPolicy('block');
    const container = renderSurface(`![demo](${PNG_1X1})`);

    expect(container.querySelector('img')).toHaveAttribute('src', PNG_1X1);
  });

  it('block: legacy absolute att.url on the deployment CDN domain still renders', () => {
    setPolicy('block', [], 'goosar-static.example.com');
    const container = renderSurface(`![old](${LEGACY_CDN_URL})`);

    expect(container.querySelector('img')).toHaveAttribute('src', LEGACY_CDN_URL);
    expect(container.querySelector('[data-blocked-image]')).toBeNull();
  });
});

describe('RichContent attachment integration under block mode', () => {
  it('passes the persisted ref through so the <Attachment> swap input is intact', () => {
    setPolicy('block');
    const attachments = [
      {
        id: '0195c9a1-1111-7bbb-8ccc-abcdefabcdef',
        filename: 'screenshot.png',
        content_type: 'image/png',
        size_bytes: 123,
        url: '/uploads/ws1/screenshot.png',
        download_url: 'https://goosar-static.example.com/uploads/ws1/screenshot.png?exp=1&sig=abc',
      },
    ];
    const { container } = render(
      <RichContent
        content={`![screenshot](${ATTACHMENT_REF})`}
        attachments={attachments as unknown as Attachment[]}
      />,
    );

    const img = container.querySelector('img[data-attachment-layer]');
    expect(img).toHaveAttribute('src', ATTACHMENT_REF);
  });
});

describe('markdownUrlTransform src gate', () => {
  afterEach(() => resetMarkdownImagePolicy());

  it('never alters attachment refs, in any mode', () => {
    for (const mode of ['allow', 'block', 'allowlist'] as const) {
      setMarkdownImagePolicy({ mode, hosts: [] });
      expect(markdownUrlTransform(ATTACHMENT_REF, 'src')).toBe(ATTACHMENT_REF);
      expect(markdownUrlTransform('/uploads/a/b.png', 'src')).toBe('/uploads/a/b.png');
    }
  });

  it('gates only image sources — href behavior is unchanged in block mode', () => {
    setMarkdownImagePolicy({ mode: 'block', hosts: [] });
    expect(markdownUrlTransform('https://example.com/page', 'href')).toBe(
      'https://example.com/page',
    );
    expect(markdownUrlTransform('https://example.com/x.png', 'src')).toBe('');
  });

  it('matches allowlisted hosts case-insensitively and ignores ports', () => {
    setMarkdownImagePolicy({ mode: 'allowlist', hosts: ['CDN.Example.com'] });
    expect(markdownUrlTransform('https://cdn.example.com:8443/x.png', 'src')).toBe(
      'https://cdn.example.com:8443/x.png',
    );
    expect(markdownUrlTransform('https://sub.cdn.example.com/x.png', 'src')).toBe('');
  });
});
