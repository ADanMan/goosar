'use client';

import { Search, SquarePen, ChevronRight } from 'lucide-react';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { useIssueDraftStore } from '@goosar/core/issues/stores/draft-store';
import { openCreateIssueWithPreference } from '@goosar/core/issues/stores/create-mode-store';
import { useShortcut } from '@goosar/core/shortcuts';
import { useSearchStore } from '../search/search-store';
import { ShortcutKeycaps } from '../common/shortcut-keycaps';
import { useT } from '../i18n';
import { NAV_LABEL_KEYS_FOR_BREADCRUMB, type NavSection } from './nav-sections';

// ponytail: `search` namespace already ships `trigger.label` ("Поиск"/"Search")
// for this exact button on the old sidebar — reused as-is, no new locale key.

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => s.hasDraft());
  if (!hasDraft) return null;
  return <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />;
}

interface CommandBarProps {
  activeSection: NavSection | null;
}

// Командная строка (T-005): поиск (⌘K, открывает существующий SearchCommand
// через search-store), «Новая задача» (хоткей C, тот же путь создания что и
// раньше в app-sidebar) и хлебные крошки воркспейс › раздел.
export function CommandBar({ activeSection }: CommandBarProps) {
  const { t } = useT('layout');
  const { t: tSearch } = useT('search');
  const workspace = useCurrentWorkspace();
  const searchShortcut = useShortcut('openSearch');
  const createIssueShortcut = useShortcut('createIssue');

  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-sidebar-border px-3">
      <nav aria-label="breadcrumb" className="flex min-w-0 items-center gap-1 text-sm text-muted-foreground">
        <span className="truncate font-medium text-foreground">{workspace?.name ?? 'Goosar'}</span>
        {activeSection && (
          <>
            <ChevronRight className="size-3.5 shrink-0" />
            <span className="truncate">{t(($) => $.nav[NAV_LABEL_KEYS_FOR_BREADCRUMB[activeSection]])}</span>
          </>
        )}
      </nav>

      <div className="ml-auto flex items-center gap-1.5">
        <button
          type="button"
          onClick={() => useSearchStore.getState().setOpen(true)}
          aria-label={tSearch(($) => $.trigger.label)}
          className="flex h-8 items-center gap-2 rounded-md px-2.5 text-sm text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        >
          <Search className="size-4" />
          {searchShortcut ? (
            <ShortcutKeycaps shortcut={searchShortcut} decorative className="pointer-events-none" />
          ) : null}
        </button>
        <button
          type="button"
          onClick={() => openCreateIssueWithPreference()}
          className="flex h-8 items-center gap-2 rounded-md bg-brand px-2.5 text-sm text-brand-foreground hover:bg-brand/90"
        >
          <span className="relative">
            <SquarePen className="size-4" />
            <DraftDot />
          </span>
          <span>{t(($) => $.sidebar.new_issue)}</span>
          {createIssueShortcut ? (
            <ShortcutKeycaps
              shortcut={createIssueShortcut}
              decorative
              className="pointer-events-none text-brand-foreground/70"
            />
          ) : null}
        </button>
      </div>
    </div>
  );
}
