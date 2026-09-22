/**
 * Локализация подписи заглушки заблокированного внешнего изображения.
 *
 * Компонент `BlockedImagePlaceholder` не имеет доступа к i18n, поэтому
 * локализованные строки пробрасываются через сеттер модуля `@goosar/ui/markdown`
 * мостом `useSyncBlockedImageLabel`, читающим активную локаль.
 *
 * Тесты проверяют полный путь с реальным i18n-провайдером и реальными
 * бандлами `editor.json`: подпись должна появляться на aria-label, видимом
 * тексте и title, а без подключённого моста — оставаться английской по
 * умолчанию.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render } from '@testing-library/react';
import { BlockedImagePlaceholder } from '@goosar/ui/markdown';
import { resetBlockedImageLabels } from '@goosar/ui/markdown/blocked-image-label';
import type { SupportedLocale } from '@goosar/core/i18n';
import { renderWithI18n } from '../test/i18n';
import { useSyncBlockedImageLabel } from './use-blocked-image-label';

function Harness({ alt }: { alt?: string }) {
  useSyncBlockedImageLabel();
  return <BlockedImagePlaceholder alt={alt} />;
}

function placeholder(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('[data-blocked-image]');
  if (el == null) throw new Error('blocked-image placeholder not rendered');
  return el;
}

afterEach(() => {
  cleanup();
  resetBlockedImageLabels();
});

const CASES: ReadonlyArray<{
  locale: SupportedLocale;
  ariaLabel: string;
  text: string;
}> = [
  { locale: 'en', ariaLabel: 'Blocked external image', text: 'External image blocked' },
  {
    locale: 'ru',
    ariaLabel: 'Заблокированное внешнее изображение',
    text: 'Внешнее изображение заблокировано',
  },
  { locale: 'zh-Hans', ariaLabel: '已屏蔽的外部图片', text: '外部图片已屏蔽' },
  { locale: 'ko', ariaLabel: '차단된 외부 이미지', text: '외부 이미지가 차단됨' },
  { locale: 'ja', ariaLabel: 'ブロックされた外部画像', text: '外部画像はブロックされました' },
];

describe('blocked-image placeholder — injected label reaches aria-label', () => {
  it.each(CASES)(
    '$locale: aria-label + visible text come from the injected editor bundle',
    ({ locale, ariaLabel, text }) => {
      const { container } = renderWithI18n(<Harness alt="" />, { locale });
      const el = placeholder(container);

      expect(el.getAttribute('aria-label')).toBe(ariaLabel);
      expect(el.textContent).toContain(text);
      expect(el.getAttribute('title') ?? '').not.toBe('');
    },
  );

  it('author alt text wins over the injected label for the accessible name', () => {
    const { container } = renderWithI18n(<Harness alt="architecture diagram" />, {
      locale: 'ru',
    });
    const el = placeholder(container);

    expect(el.getAttribute('aria-label')).toBe('architecture diagram');
    expect(el.textContent).toContain('architecture diagram');
  });
});

describe('blocked-image placeholder — English default when nothing injected', () => {
  it('renders the built-in English strings with no bridge mounted', () => {
    const { container } = render(<BlockedImagePlaceholder alt="" />);
    const el = placeholder(container);

    expect(el.getAttribute('aria-label')).toBe('Blocked external image');
    expect(el.textContent).toContain('External image blocked');
    expect(el.getAttribute('title')).toBe(
      "External image blocked by this deployment's image policy",
    );
  });
});
