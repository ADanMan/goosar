// Хук: ref FlatList и прокрутка к началу при изменении наблюдаемого значения
// (UISearchController сам этого не делает).
import { useEffect, useRef } from 'react';
import type { FlatList } from 'react-native';

export function useScrollToTopOnChange<T>(value: T) {
  const ref = useRef<FlatList<any>>(null);

  useEffect(() => {
    ref.current?.scrollToOffset({ offset: 0, animated: false });
  }, [value]);

  return ref;
}
