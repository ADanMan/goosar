// Состояние «ответить на комментарий X»: пишет меню долгого нажатия,
// читает встроенный композер.
import { create } from "zustand";

export interface ReplyTarget {
  commentId: string;
  actorName: string;
  preview: string;
}

interface State {
  target: ReplyTarget | null;
  setTarget: (target: ReplyTarget) => void;
  clear: () => void;
}

export const useReplyTargetStore = create<State>((set) => ({
  target: null,
  setTarget: (target) => set({ target }),
  clear: () => set({ target: null }),
}));
