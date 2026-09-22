/**
 * Стор авторизации мобильного клиента (Zustand). Токен пишется только при
 * успешном verifyCode; при 401 токен сбрасывается, при прочих ошибках —
 * сохраняется для повторной попытки при следующем запуске. Хранилище —
 * expo-secure-store, отдельное от веб- и десктоп-реализаций.
 */
import { create } from "zustand";
import type { User } from "@goosar/core/types";
import { api, ApiError } from "./api";
import { clearToken, getToken, setToken } from "./secure-storage";
import { useWorkspaceStore } from "./workspace-store";

interface AuthState {
  user: User | null;
  isLoading: boolean;
  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  logout: () => Promise<void>;
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,

  initialize: async () => {
    await useWorkspaceStore.getState().restoreSlug();

    const token = await getToken();
    if (!token) {
      set({ isLoading: false });
      return;
    }
    api.setToken(token);
    try {
      const user = await api.getMe();
      set({ user, isLoading: false });
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        await clearToken();
        api.setToken(null);
      }
      set({ user: null, isLoading: false });
    }
  },

  sendCode: async (email) => {
    await api.sendCode(email);
  },

  verifyCode: async (email, code) => {
    const { token, user } = await api.verifyCode(email, code);
    await setToken(token);
    api.setToken(token);
    set({ user });
    return user;
  },

  logout: async () => {
    await clearToken();
    api.setToken(null);
    set({ user: null });
  },

  setUser: (user) => set({ user }),
}));
