/**
 * Скелетон загрузки для списков задач (мои задачи, задачи воркспейса).
 * Оба экрана группируют задачи по статусу через SectionList — скелетон
 * повторяет эту форму, чтобы глаз сразу видел список, а не спиннер по
 * центру. Ряд-скелетон повторяет верстку IssueRow, заголовок секции —
 * верстку SectionHeader.
 */
import { View } from 'react-native';
import { Skeleton } from '@/components/ui/skeleton';

export function IssuesLoading() {
  return (
    <View className="pt-2">
      {Array.from({ length: 2 }).map((_, sectionIdx) => (
        <View key={sectionIdx} className="pb-2">
          <View className="px-4 py-2 flex-row items-center gap-2">
            <Skeleton className="size-3.5 rounded-full" />
            <Skeleton className="h-3 w-20" />
          </View>
          {Array.from({ length: 3 }).map((_, rowIdx) => (
            <View key={rowIdx} className="flex-row items-center gap-3 px-4 py-3">
              <Skeleton className="size-3.5 rounded-full" />
              <Skeleton className="h-3 w-14" />
              <Skeleton className="h-3.5 flex-1" />
              <Skeleton className="size-6 rounded-full" />
            </View>
          ))}
        </View>
      ))}
    </View>
  );
}
