/**
 * Поздняя загрузка конфигурации CDN должна переобрабатывать уже
 * отрендеренный контент.
 *
 * Домен CDN приходит асинхронно после авторизации. Определение карточек
 * файлов происходит на шаге препроцессинга markdown, который раньше читал
 * домен один раз при рендере — контент, отрендеренный до прихода конфига,
 * навсегда оставался со старыми CDN-ссылками как обычными якорями.
 *
 * Тест использует реальный стор: рендерит с пустым доменом, затем публикует
 * конфиг так же, как это делает инициализация авторизации, и проверяет, что
 * блок обновляется.
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render } from '@testing-library/react';
import { configStore } from '@goosar/core/config';

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
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => 'acme',
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

vi.mock('../editor/attachment', () => ({
  Attachment: ({ attachment }: { attachment: { url: string; filename: string } }) => (
    <span data-testid="file-card" data-url={attachment.url}>
      {attachment.filename}
    </span>
  ),
}));

import { RichContent } from './rich-content';

const CDN_DOMAIN = 'goosar-static.example.com';
const FILE_URL = `https://${CDN_DOMAIN}/workspaces/w1/files/report.pdf`;
const CONTENT = `Attached:\n\n[report.pdf](${FILE_URL})`;

beforeEach(() => {
  configStore.setState({ cdnDomain: '', cdnSigned: false });
});

describe('RichContent CDN config reactivity', () => {
  it('upgrades a legacy CDN link to a file card when the config arrives late', () => {
    const { container } = render(<RichContent content={CONTENT} />);

    expect(container.querySelector("[data-testid='file-card']")).toBeNull();
    expect(container.querySelector(`a[href="${FILE_URL}"]`)).not.toBeNull();

    act(() => {
      configStore.getState().setCdnConfig({ cdnDomain: CDN_DOMAIN });
    });

    const card = container.querySelector("[data-testid='file-card']");
    expect(card).not.toBeNull();
    expect(card?.getAttribute('data-url')).toBe(FILE_URL);
  });

  it('renders the file card immediately when the config is already present', () => {
    configStore.setState({ cdnDomain: CDN_DOMAIN });

    const { container } = render(<RichContent content={CONTENT} />);

    expect(container.querySelector("[data-testid='file-card']")).not.toBeNull();
  });

  it('leaves non-CDN links alone when the config arrives', () => {
    const external = 'Attached:\n\n[the spec](https://example.com/spec.pdf)';
    const { container } = render(<RichContent content={external} />);

    act(() => {
      configStore.getState().setCdnConfig({ cdnDomain: CDN_DOMAIN });
    });

    expect(container.querySelector("[data-testid='file-card']")).toBeNull();
    expect(container.querySelector('a[href="https://example.com/spec.pdf"]')).not.toBeNull();
  });
});
