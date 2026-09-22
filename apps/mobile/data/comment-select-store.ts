/**
 * Режим выделения текста для комментариев. При выборе "Select Text" id
 * комментария сохраняется здесь; `CommentBody` реагирует, снимая обработчик
 * долгого нажатия и включая выделение текста в Markdown-теле. Активным
 * может быть только один комментарий сразу, поэтому нужен отдельный стор.
 * Сбрасывается по кнопке "Готово", при скролле ленты или уходе с экрана.
 */
import { create } from "zustand";

interface State {
  selectingId: string | null;
  setSelecting: (commentId: string) => void;
  clear: () => void;
}

export const useCommentSelectStore = create<State>((set) => ({
  selectingId: null,
  setSelecting: (commentId) => set({ selectingId: commentId }),
  clear: () => set({ selectingId: null }),
}));
