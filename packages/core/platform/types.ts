import type { LocaleAdapter, LocaleResources, SupportedLocale } from '../i18n';
import type { StorageAdapter } from '../types/storage';

export interface ClientIdentity {
  platform?: string;
  version?: string;
  os?: string;
}

export interface CoreProviderProps {
  children: React.ReactNode;
  apiBaseUrl?: string;
  wsUrl?: string;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
  onLogin?: () => void;
  onLogout?: () => void;
  identity?: ClientIdentity;
  locale: SupportedLocale;
  resources: Record<string, LocaleResources>;
  localeAdapter?: LocaleAdapter;
}
