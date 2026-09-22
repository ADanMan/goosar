import type { WebContents } from 'electron';

export type ShortcutInput = {
  type: string;
  key: string;
  control: boolean;
  meta: boolean;
  alt: boolean;
  shift: boolean;
  isAutoRepeat?: boolean;
};

export type ZoomTarget = Pick<WebContents, 'getZoomLevel' | 'setZoomLevel'>;

const ZOOM_STEP = 0.5;
const ZOOM_MIN = -3;
const ZOOM_MAX = 4.5;

export type ShortcutResult = boolean | 'close-tab';

export function handleAppShortcut(
  input: ShortcutInput,
  webContents: ZoomTarget,
  platform: NodeJS.Platform = process.platform,
): ShortcutResult {
  if (input.type !== 'keyDown') return false;
  const primary = platform === 'darwin' ? input.meta : input.control;
  const secondary = platform === 'darwin' ? input.control : input.meta;
  const noSecondaryModifiers = !secondary && !input.alt;

  if ((primary && input.key.toLowerCase() === 'r') || input.key === 'F5') {
    return true;
  }

  if (!primary || !noSecondaryModifiers) return false;

  if ((input.key === '=' && !input.shift) || (input.key === '+' && input.shift)) {
    const next = Math.min(webContents.getZoomLevel() + ZOOM_STEP, ZOOM_MAX);
    webContents.setZoomLevel(next);
    return true;
  }

  if ((input.key === '-' && !input.shift) || (input.key === '_' && input.shift)) {
    const next = Math.max(webContents.getZoomLevel() - ZOOM_STEP, ZOOM_MIN);
    webContents.setZoomLevel(next);
    return true;
  }

  if (input.key === '0' && !input.shift) {
    webContents.setZoomLevel(0);
    return true;
  }

  if (input.key.toLowerCase() === 'w' && !input.shift) {
    if (input.isAutoRepeat) return true;
    return 'close-tab';
  }

  return false;
}
