import { useSyncExternalStore } from 'react';

function subscribe(onStoreChange: () => void): () => void {
  if (typeof document === 'undefined') return () => {};
  document.addEventListener('visibilitychange', onStoreChange);
  window.addEventListener('focus', onStoreChange);
  window.addEventListener('blur', onStoreChange);
  return () => {
    document.removeEventListener('visibilitychange', onStoreChange);
    window.removeEventListener('focus', onStoreChange);
    window.removeEventListener('blur', onStoreChange);
  };
}

function getSnapshot(): boolean {
  if (typeof document === 'undefined') return true;
  return document.visibilityState === 'visible' && document.hasFocus();
}

function getServerSnapshot(): boolean {
  return true;
}

export function useAppForeground(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
