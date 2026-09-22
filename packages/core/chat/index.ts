export {
  createChatStore,
  CHAT_MIN_W,
  CHAT_MIN_H,
  CHAT_DEFAULT_W,
  CHAT_DEFAULT_H,
  DRAFT_NEW_SESSION,
} from './store';
export type { ChatStoreOptions, ChatState, ChatTimelineItem } from './store';
export { useRecentContextStore, selectRecentContexts } from './recent-context-store';
export type { RecentContextEntry, RecentContextType } from './recent-context-store';

import type { createChatStore as CreateChatStoreFn } from './store';

type ChatStoreInstance = ReturnType<typeof CreateChatStoreFn>;

let _store: ChatStoreInstance | null = null;

export function registerChatStore(store: ChatStoreInstance) {
  _store = store;
}

export const useChatStore: ChatStoreInstance = new Proxy(
  (() => {}) as unknown as ChatStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store) throw new Error('Chat store not initialised — call registerChatStore() first');
      return (_store as unknown as (...a: unknown[]) => unknown)(...args);
    },
    get(_target, prop) {
      if (!_store) return undefined;
      return Reflect.get(_store, prop);
    },
  },
);
