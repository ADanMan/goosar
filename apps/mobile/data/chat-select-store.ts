/**
 * Режим выделения текста для сообщений чата. При выборе "Select Text" в
 * меню долгого нажатия id сообщения сохраняется здесь; строка сообщения
 * реагирует, снимая обработчик долгого нажатия и включая выделение текста.
 * Отдельный стор нужен, так как активным может быть только одно сообщение,
 * а переключение происходит из другого поддерева компонентов. Сбрасывается
 * по кнопке "Готово", при скролле списка или уходе с вкладки чата.
 */
import { create } from "zustand";

interface State {
  selectingId: string | null;
  setSelecting: (messageId: string) => void;
  clear: () => void;
}

export const useChatSelectStore = create<State>((set) => ({
  selectingId: null,
  setSelecting: (messageId) => set({ selectingId: messageId }),
  clear: () => set({ selectingId: null }),
}));
