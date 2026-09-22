import { create } from 'zustand';
import type { User, StorageAdapter } from '../types';
import { identify as identifyAnalytics, resetAnalytics } from '../analytics';
import type { ApiClient } from '../api/client';
import { setCurrentWorkspace } from '../platform/workspace-storage';

export class MFARequiredError extends Error {
  readonly mfaToken: string;

  constructor(mfaToken: string) {
    super('A second factor is required to finish signing in.');
    this.name = 'MFARequiredError';
    this.mfaToken = mfaToken;
  }
}

export interface AuthStoreOptions {
  api: ApiClient;
  storage: StorageAdapter;
  onLogin?: () => void;
  onLogout?: () => void;
  cookieAuth?: boolean;
}

export interface AuthState {
  user: User | null;
  isLoading: boolean;
  pendingMfaToken: string | null;

  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  loginWithLinkToken: (linkToken: string) => Promise<User>;
  loginWithToken: (token: string) => Promise<User>;
  logout: () => void;
  setPendingMfaToken: (token: string | null) => void;
  setUser: (user: User) => void;
  refreshMe: () => Promise<void>;
}

export function createAuthStore(options: AuthStoreOptions) {
  const { api, storage, onLogin, onLogout, cookieAuth } = options;

  return create<AuthState>((set) => ({
    user: null,
    isLoading: true,
    pendingMfaToken: null,

    sendCode: async (email: string) => {
      await api.sendCode(email);
    },

    verifyCode: async (email: string, code: string) => {
      const { token, user, mfa_required, mfa_token } = await api.verifyCode(email, code);
      if (mfa_required) {
        throw new MFARequiredError(mfa_token);
      }
      if (!user?.id) {
        throw new Error('Login failed: server returned a malformed response.');
      }
      if (!cookieAuth) {
        if (!token) {
          throw new Error('Login failed: server did not return an authentication token.');
        }
        storage.setItem('goosar_token', token);
        api.setToken(token);
      }
      onLogin?.();
      identifyAnalytics(user.id);
      set({ user });
      return user;
    },

    loginWithLinkToken: async (linkToken: string) => {
      const { token, user, mfa_required, mfa_token } = await api.verifyLink(linkToken);
      if (mfa_required) {
        throw new MFARequiredError(mfa_token);
      }
      if (!user?.id) {
        throw new Error('Login failed: server returned a malformed response.');
      }
      if (!cookieAuth) {
        if (!token) {
          throw new Error('Login failed: server did not return an authentication token.');
        }
        storage.setItem('goosar_token', token);
        api.setToken(token);
      }
      onLogin?.();
      identifyAnalytics(user.id);
      set({ user, isLoading: false });
      return user;
    },

    loginWithToken: async (token: string) => {
      storage.setItem('goosar_token', token);
      api.setToken(token);
      const user = await api.getMe();
      onLogin?.();
      identifyAnalytics(user.id);
      set({ user, isLoading: false });
      return user;
    },

    logout: () => {
      if (cookieAuth) {
        api.logout().catch(() => {});
      }
      storage.removeItem('goosar_token');
      api.setToken(null);
      setCurrentWorkspace(null, null);
      resetAnalytics();
      onLogout?.();
      set({ user: null });
    },

    setPendingMfaToken: (token: string | null) => set({ pendingMfaToken: token }),

    setUser: (user: User) => {
      set({ user });
    },

    refreshMe: async () => {
      const user = await api.getMe();
      set({ user });
    },
  }));
}
