/**
 * Общие кнопки хедера для основных вкладок (Inbox / My Issues): поиск и
 * создание issue. Меню воркспейса вынесено в таб «More». Действия,
 * специфичные для конкретной вкладки, сюда не добавляются.
 */

import { router } from 'expo-router';
import { IconButton } from '@/components/ui/icon-button';
import { useWorkspaceStore } from '@/data/workspace-store';

export function HeaderActions() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const onSearch = () => {
    if (slug) router.push(`/${slug}/search`);
  };
  const onCreate = () => {
    if (slug) router.push(`/${slug}/new-issue`);
  };

  return (
    <>
      <IconButton name="search" onPress={onSearch} accessibilityLabel="Search" />
      <IconButton name="add" iconSize={24} onPress={onCreate} accessibilityLabel="New issue" />
    </>
  );
}
