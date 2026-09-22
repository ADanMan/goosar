import { create } from 'zustand';

export type WindowOverlay =
  | { type: 'new-workspace' }
  // The deployment's role picker (#487, R-4). A pre-workspace transition
  // like its neighbours, so it is overlay state and not a route.
  | { type: 'join-workspace' }
  | { type: 'invite'; invitationId: string }
  | { type: 'invitations' }
  | { type: 'onboarding' };

interface WindowOverlayStore {
  overlay: WindowOverlay | null;
  open: (overlay: WindowOverlay) => void;
  close: () => void;
}

export const useWindowOverlayStore = create<WindowOverlayStore>((set) => ({
  overlay: null,
  open: (overlay) => set({ overlay }),
  close: () => set({ overlay: null }),
}));
