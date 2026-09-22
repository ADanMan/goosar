import { create } from 'zustand';

export interface WelcomeSignal {
  workspaceId: string;
  choice: 'runtime' | 'skip';
  runtimeId?: string;
}

interface WelcomeStoreState {
  signal: WelcomeSignal | null;
  dismissed: boolean;
  set: (signal: WelcomeSignal) => void;
  dismiss: () => void;
  reset: () => void;
}

export const useWelcomeStore = create<WelcomeStoreState>((set) => ({
  signal: null,
  dismissed: false,
  set: (signal) => set({ signal, dismissed: false }),
  dismiss: () => set({ dismissed: true }),
  reset: () => set({ signal: null, dismissed: false }),
}));
