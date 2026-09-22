/**
 * Стор активного workspace (id + slug) на Zustand. Slug сохраняется в
 * SecureStore, чтобы при холодном старте восстанавливать последний выбор.
 *
 * Источник истины — роут (`/[workspace]/...`); стор — быстрый синхронный
 * кэш, из которого ApiClient.fetch берёт заголовок X-Workspace-Slug.
 */
import { create } from "zustand";
import * as SecureStore from "expo-secure-store";

const SLUG_KEY = "goosar_current_workspace_slug";

interface WorkspaceState {
  currentWorkspaceId: string | null;
  currentWorkspaceSlug: string | null;
  setCurrentWorkspace: (id: string, slug: string) => Promise<void>;
  restoreSlug: () => Promise<string | null>;
  clear: () => Promise<void>;
}

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  currentWorkspaceId: null,
  currentWorkspaceSlug: null,

  setCurrentWorkspace: async (id, slug) => {
    set({ currentWorkspaceId: id, currentWorkspaceSlug: slug });
    await SecureStore.setItemAsync(SLUG_KEY, slug);
  },

  restoreSlug: async () => {
    const slug = await SecureStore.getItemAsync(SLUG_KEY);
    if (slug) set({ currentWorkspaceSlug: slug });
    return slug;
  },

  clear: async () => {
    set({ currentWorkspaceId: null, currentWorkspaceSlug: null });
    await SecureStore.deleteItemAsync(SLUG_KEY);
  },
}));

export function getCurrentSlug(): string | null {
  return useWorkspaceStore.getState().currentWorkspaceSlug;
}
