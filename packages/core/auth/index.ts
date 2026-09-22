export { createAuthStore, MFARequiredError } from './store';
export type { AuthStoreOptions, AuthState } from './store';
export { sanitizeNextUrl } from './utils';

import type { createAuthStore as CreateAuthStoreFn } from './store';

type AuthStoreInstance = ReturnType<typeof CreateAuthStoreFn>;

let _store: AuthStoreInstance | null = null;

export function registerAuthStore(store: AuthStoreInstance) {
  _store = store;
}

export const useAuthStore: AuthStoreInstance = new Proxy(
  (() => {}) as unknown as AuthStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store) throw new Error('Auth store not initialised — call registerAuthStore() first');
      return (_store as unknown as (...a: unknown[]) => unknown)(...args);
    },
    get(_target, prop) {
      if (!_store) return undefined;
      return Reflect.get(_store, prop);
    },
  },
);
