// Обёртка над useColorScheme NativeWind с сохранением выбора темы
// в expo-secure-store.
import { useColorScheme as useNativewindColorScheme } from 'nativewind';
import { useEffect, useState } from 'react';
import * as SecureStore from 'expo-secure-store';

const STORAGE_KEY = 'theme-preference';

export type ThemePreference = 'light' | 'dark' | 'system';

export function useColorScheme() {
  const { colorScheme, setColorScheme: applyScheme } = useNativewindColorScheme();
  const [preference, setPreferenceState] = useState<ThemePreference>('system');

  useEffect(() => {
    let cancelled = false;
    SecureStore.getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (cancelled) return;
        if (saved === 'light' || saved === 'dark' || saved === 'system') {
          setPreferenceState(saved);
          applyScheme(saved);
        }
      })
      .catch(() => {
        // Read failures are non-fatal; keep default 'system'.
      });
    return () => {
      cancelled = true;
    };
  }, [applyScheme]);

  const setPreference = (p: ThemePreference) => {
    setPreferenceState(p);
    applyScheme(p);
    void SecureStore.setItemAsync(STORAGE_KEY, p);
  };

  return {
    colorScheme: colorScheme ?? 'light',
    preference,
    setPreference,
    isDarkColorScheme: colorScheme === 'dark',
  };
}
