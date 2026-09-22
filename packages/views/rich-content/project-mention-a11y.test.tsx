/**
 * Доступность упоминания проекта.
 *
 * Упоминание — это ссылка. Она должна быть достижима табом, активируема
 * Enter и содержать настоящий URL, а не `<span onClick>`, который реагирует
 * только на мышь.
 *
 * Это регрессионный тест, а не гипотетический случай: упоминания проекта
 * раньше рендерились через AppLink в чате, а readonly-рендерер использовал
 * span с обработчиком клика; объединение поверхностей незаметно
 * распространило span на чат. Span и якорь визуально неотличимы и одинаково
 * ведут себя под мышью, поэтому поймать это может только проверка типа
 * отрендеренного элемента.
 *
 * Используются настоящие AppLink и NavigationProvider намеренно — мок
 * AppLink, рендерящий `<a>`, проверял бы сам мок, а не компонент.
 */

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { NavigationProvider } from '../navigation/context';
import type { NavigationAdapter } from '../navigation/types';

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

vi.mock('../issues/components/issue-mention-card', () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));

vi.mock('../projects/components/project-chip', () => ({
  ProjectChip: ({ projectId, fallbackLabel }: { projectId: string; fallbackLabel?: string }) => (
    <span data-testid="project-chip">{fallbackLabel ?? projectId}</span>
  ),
}));

vi.mock('../editor/link-hover-card', () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

import { RichContent } from './rich-content';

const PROJECT_ID = '8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a';
const MENTION = `Tracked under [Roadmap](mention://project/${PROJECT_ID}).`;

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: '/',
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderMention(adapter: NavigationAdapter = makeAdapter()) {
  return render(
    <NavigationProvider value={adapter}>
      <RichContent content={MENTION} />
    </NavigationProvider>,
  );
}

describe('project mention accessibility', () => {
  it('renders an anchor with the project href', () => {
    const { container } = renderMention();

    const anchor = container.querySelector(`a[href="/acme/projects/${PROJECT_ID}"]`);
    expect(anchor).not.toBeNull();
    expect(anchor?.tagName).toBe('A');
    expect(screen.getByTestId('project-chip')).toBeInTheDocument();
  });

  it('does not render the chip as a click-only span', () => {
    const chip = renderMention().getByTestId('project-chip');
    expect(chip.closest('a')).not.toBeNull();
  });

  it('is keyboard focusable', async () => {
    const user = userEvent.setup();
    const { container } = renderMention();
    const anchor = container.querySelector('a') as HTMLAnchorElement;

    await user.tab();

    expect(document.activeElement).toBe(anchor);
  });

  it('activates on Enter', async () => {
    const push = vi.fn();
    const user = userEvent.setup();
    renderMention(makeAdapter({ push }));

    await user.tab();
    await user.keyboard('{Enter}');

    expect(push).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`);
  });

  it('navigates on click through the adapter', () => {
    const push = vi.fn();
    const { container } = renderMention(makeAdapter({ push }));

    fireEvent.click(container.querySelector('a') as HTMLAnchorElement);

    expect(push).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`);
  });

  it('opens in a new tab on modifier-click, labelled with the mention text', () => {
    const openInNewTab = vi.fn();
    const { container } = renderMention(makeAdapter({ openInNewTab }));

    fireEvent.click(container.querySelector('a') as HTMLAnchorElement, { metaKey: true });

    expect(openInNewTab).toHaveBeenCalledWith(`/acme/projects/${PROJECT_ID}`, 'Roadmap');
  });
});
