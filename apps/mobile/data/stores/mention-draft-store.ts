// Межмаршрутный стор черновика @упоминаний для композера комментария:
// пикер живёт в своём formSheet и не может делить колбэки с композером.
import { create } from "zustand";

export type MentionTargetType = "member" | "agent" | "squad" | "all" | "issue";

export interface MentionChipDraft {
  type: MentionTargetType;
  id: string;
  name: string;
}

function sameMention(
  a: MentionChipDraft,
  b: { type: MentionTargetType; id: string },
) {
  return a.type === b.type && a.id === b.id;
}

interface State {
  mentions: MentionChipDraft[];
  toggle: (mention: MentionChipDraft) => void;
  remove: (type: MentionTargetType, id: string) => void;
  clear: () => void;
}

export const useMentionDraftStore = create<State>((set) => ({
  mentions: [],
  toggle: (mention) =>
    set((s) => {
      const existing = s.mentions.some((m) => sameMention(m, mention));
      if (existing) {
        return {
          mentions: s.mentions.filter((m) => !sameMention(m, mention)),
        };
      }
      return { mentions: [...s.mentions, mention] };
    }),
  remove: (type, id) =>
    set((s) => ({
      mentions: s.mentions.filter((m) => !sameMention(m, { type, id })),
    })),
  clear: () => set({ mentions: [] }),
}));
