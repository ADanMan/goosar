// Черновики чата по сессиям, только в памяти: переживают переключение вкладок,
// но не холодный старт.
import { create } from "zustand";

export const DRAFT_NEW_SESSION = "__new__";

interface ChatDraftsState {
  drafts: Record<string, string>;
  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  promoteNewDraft: (newSessionId: string) => void;
}

export const useChatDraftsStore = create<ChatDraftsState>((set, get) => ({
  drafts: {},
  setDraft: (sessionId, text) => {
    const current = get().drafts;
    if (current[sessionId] === text) return;
    if (text === "") {
      if (!(sessionId in current)) return;
      const next = { ...current };
      delete next[sessionId];
      set({ drafts: next });
      return;
    }
    set({ drafts: { ...current, [sessionId]: text } });
  },
  clearDraft: (sessionId) => {
    const current = get().drafts;
    if (!(sessionId in current)) return;
    const next = { ...current };
    delete next[sessionId];
    set({ drafts: next });
  },
  promoteNewDraft: (newSessionId) => {
    const current = get().drafts;
    const pending = current[DRAFT_NEW_SESSION];
    if (!pending) return;
    const next = { ...current, [newSessionId]: pending };
    delete next[DRAFT_NEW_SESSION];
    set({ drafts: next });
  },
}));
