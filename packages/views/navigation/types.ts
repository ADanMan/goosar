export interface NavigationAdapter {
  push(path: string): void;
  replace(path: string): void;
  back(): void;
  pathname: string;
  searchParams: URLSearchParams;
  openInNewTab?: (path: string, title?: string, opts?: { activate?: boolean }) => void;
  getShareableUrl: (path: string) => string;
  prefetch?: (path: string) => void;
  canGoBack?: () => boolean;
}
