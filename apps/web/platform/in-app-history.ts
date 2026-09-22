// «Есть ли за этой страницей другая страница Goosar?» для навигационного адаптера
// веба: шаг назад не должен уводить из приложения при холодном открытии.

type NavigationApi = { canGoBack: boolean };

export function canGoBackInApp(): boolean {
  if (typeof window === 'undefined') return false;
  const navigation = (window as { navigation?: unknown }).navigation as NavigationApi | undefined;
  return navigation?.canGoBack === true;
}
