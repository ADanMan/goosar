import type { Translations } from 'fumadocs-ui/i18n';
import type { Lang } from './i18n';

// Fumadocs built-in UI strings (search, TOC, last-updated, etc.) per locale.
// English uses Fumadocs defaults so we only override Russian.
export const uiTranslations: Partial<Record<Lang, Partial<Translations>>> = {
  ru: {
    search: 'Поиск',
    searchNoResult: 'Ничего не найдено',
    toc: 'На этой странице',
    tocNoHeadings: 'Заголовков нет',
    lastUpdate: 'Обновлено',
    chooseLanguage: 'Выбрать язык',
    nextPage: 'Следующая страница',
    previousPage: 'Предыдущая страница',
    chooseTheme: 'Сменить тему',
    editOnGithub: 'Править на GitHub',
  },
};

// Display name shown in the LanguageToggle dropdown.
export const localeLabels: Record<Lang, string> = {
  ru: 'Русский',
  en: 'English',
};

// Copy for the welcome page (Hero + Byline). Pages are translated as MDX;
// this dict only carries TSX-rendered chrome above the MDX body.
export const homeCopy = {
  ru: {
    eyebrow: 'Документация Goosar',
    titleLead: 'Люди и агенты —',
    titleAccent: ' в одном месте.',
    byline: ['С чего начать', 'Обновлено в сентябре 2026', '3 минуты чтения'],
  },
  en: {
    eyebrow: 'Goosar Docs',
    titleLead: 'Humans and agents,',
    titleAccent: ' in one place.',
    byline: ['Developers', 'Updated September 2026', '3 min read'],
  },
} as const satisfies Record<Lang, unknown>;
