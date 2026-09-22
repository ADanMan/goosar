// Хук подключения нативного UISearchController к маршруту; возвращает
// текущую строку запроса.
import { useLayoutEffect, useState } from 'react';
import { useNavigation } from 'expo-router';
import type { NativeSyntheticEvent, TextInputFocusEventData } from 'react-native';

export function useNativeSearchBar(placeholder: string, options?: { autoFocus?: boolean }): string {
  const navigation = useNavigation();
  const [query, setQuery] = useState('');
  const autoFocus = options?.autoFocus;

  useLayoutEffect(() => {
    navigation.setOptions({
      headerSearchBarOptions: {
        placeholder,
        autoCapitalize: 'none',
        hideWhenScrolling: false,
        autoFocus,
        onChangeText: (e: NativeSyntheticEvent<TextInputFocusEventData>) =>
          setQuery(e.nativeEvent.text),
        onCancelButtonPress: () => setQuery(''),
      },
    });
  }, [navigation, placeholder, autoFocus]);

  return query;
}
