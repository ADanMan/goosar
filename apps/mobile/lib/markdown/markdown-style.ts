/**
 * Объект стилей markdown для EnrichedMarkdownText, построенный на токенах
 * темы приложения — цвета автоматически следуют светлой/тёмной теме.
 *
 * Реализован как хук, а не статический объект: нативный рендерер markdown
 * принимает только императивный объект стилей и не понимает NativeWind-классы,
 * поэтому объект пересобирается при каждой смене colorScheme.
 *
 * Размеры откалиброваны под мобильную типографическую шкалу (на ступень
 * меньше веб-значений, так как заголовки внутри карточки issue — это
 * структурный, а не экранный текст).
 */
import { useMemo } from 'react';
import { THEME } from '@/lib/theme';
import { useColorScheme } from '@/lib/use-color-scheme';

const MD_FONT = {
  body: 14,
  h1: 20,
  h2: 18,
  h3: 16,
  h4: 15,
  h5: 14,
  h6: 13,
  codeBlock: 13,
} as const;

const MD_LINE = {
  body: 24,
  h1: 28,
  h2: 24,
  h3: 22,
  h4: 22,
  h5: 20,
  h6: 19,
} as const;

const MD_GAP = {
  paragraph: 12,
  headingTopLarge: 16,
  headingTopSmall: 12,
  headingBottomLarge: 8,
  headingBottomSmall: 6,
} as const;

export function useMarkdownStyle() {
  const { isDarkColorScheme } = useColorScheme();
  const t = isDarkColorScheme ? THEME.dark : THEME.light;

  return useMemo(
    () => ({
      paragraph: {
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        color: t.foreground,
        marginBottom: MD_GAP.paragraph,
      },
      h1: {
        fontSize: MD_FONT.h1,
        lineHeight: MD_LINE.h1,
        fontWeight: '700' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      },
      h2: {
        fontSize: MD_FONT.h2,
        lineHeight: MD_LINE.h2,
        fontWeight: '600' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      },
      h3: {
        fontSize: MD_FONT.h3,
        lineHeight: MD_LINE.h3,
        fontWeight: '600' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h4: {
        fontSize: MD_FONT.h4,
        lineHeight: MD_LINE.h4,
        fontWeight: '600' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h5: {
        fontSize: MD_FONT.h5,
        lineHeight: MD_LINE.h5,
        fontWeight: '600' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h6: {
        fontSize: MD_FONT.h6,
        lineHeight: MD_LINE.h6,
        fontWeight: '600' as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      strong: {
        fontWeight: 'bold' as const,
        color: t.foreground,
      },
      em: {
        fontStyle: 'italic' as const,
        color: t.foreground,
      },
      strikethrough: {
        color: t.mutedForeground,
      },
      underline: {
        color: t.foreground,
      },
      link: {
        color: t.brand,
        underline: true,
      },
      code: {
        color: t.mutedForeground,
        backgroundColor: 'transparent',
        borderColor: 'transparent',
        fontSize: MD_FONT.body,
      },
      codeBlock: {
        fontSize: MD_FONT.codeBlock,
        color: t.foreground,
        backgroundColor: t.surface2,
        borderColor: t.border,
        padding: 12,
        borderRadius: 8,
        marginBottom: MD_GAP.paragraph,
      },
      blockquote: {
        color: t.mutedForeground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        borderColor: t.border,
        borderWidth: 3,
        backgroundColor: 'transparent',
        marginBottom: MD_GAP.paragraph,
      },
      list: {
        color: t.foreground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        bulletColor: t.mutedForeground,
        bulletSize: 4,
        markerColor: t.mutedForeground,
        gapWidth: 8,
        marginLeft: 16,
      },
      image: {
        borderRadius: 8,
        marginBottom: MD_GAP.paragraph,
      },
      taskList: {
        checkedColor: t.brand,
        borderColor: t.border,
        checkmarkColor: t.brandForeground,
        checkedTextColor: t.mutedForeground,
        checkboxSize: 16,
      },
      table: {
        color: t.foreground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        borderColor: t.border,
        borderRadius: 8,
        headerBackgroundColor: t.surface2,
        headerTextColor: t.foreground,
        rowEvenBackgroundColor: 'transparent',
        rowOddBackgroundColor: 'transparent',
        cellPaddingHorizontal: 10,
        cellPaddingVertical: 6,
        marginBottom: MD_GAP.paragraph,
      },
      thematicBreak: {
        color: t.border,
        marginTop: 16,
        marginBottom: 16,
      },
      math: {
        fontSize: 16,
        color: t.foreground,
        backgroundColor: t.muted,
        padding: 12,
        marginBottom: MD_GAP.paragraph,
        textAlign: 'center' as const,
      },
      inlineMath: {
        color: t.foreground,
      },
    }),
    [t],
  );
}
