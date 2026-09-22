// Канал между вкладкой чата и формой выбора сессии: formSheet не может передать
// выбор вверх по дереву, поэтому обмен идёт через маленький стор.
import { useEffect, useRef } from "react";
import { create } from "zustand";

interface ChatSessionPickerState {
  activeSessionId: string | null;
  setActiveSessionId: (id: string | null) => void;
  selectRequest: { id: string | null; nonce: number } | null;
  requestSelect: (id: string | null) => void;
  consumeSelect: () => void;
  reset: () => void;
}

const INITIAL = {
  activeSessionId: null,
  selectRequest: null,
} as const;

export const useChatSessionPickerStore = create<ChatSessionPickerState>(
  (set, get) => ({
    ...INITIAL,
    setActiveSessionId: (id) => set({ activeSessionId: id }),
    requestSelect: (id) =>
      set({ selectRequest: { id, nonce: (get().selectRequest?.nonce ?? 0) + 1 } }),
    consumeSelect: () => set({ selectRequest: null }),
    reset: () => set({ ...INITIAL }),
  }),
);

export function useChatSessionPickerResetOnWorkspaceChange(
  wsId: string | null,
) {
  const prevRef = useRef(wsId);
  useEffect(() => {
    if (prevRef.current !== wsId) {
      useChatSessionPickerStore.getState().reset();
      prevRef.current = wsId;
    }
  }, [wsId]);
}
