import {
  getShortcutPlatform,
  getShortcutRuntime,
  type ShortcutPlatform,
  type ShortcutRuntime,
} from './platform';

export type ShortcutActionId =
  | 'openSearch'
  | 'createIssue'
  | 'toggleSidebar'
  | 'findInIssue'
  | 'send'
  | 'goInbox'
  | 'goChat'
  | 'goMyIssues'
  | 'goIssues'
  | 'goProjects'
  | 'goAutopilots'
  | 'goAgents'
  | 'goSquads'
  | 'goUsage'
  | 'goRuntimes'
  | 'goSkills'
  | 'goSettings';

export type ShortcutCategory = 'general' | 'navigation';

export interface ShortcutModifiers {
  primary: boolean;
  control: boolean;
  meta: boolean;
  alt: boolean;
  shift: boolean;
}

export interface ShortcutChord {
  key: string;
  modifiers: ShortcutModifiers;
}

export interface ShortcutActionDefinition {
  id: ShortcutActionId;
  category: ShortcutCategory;
  defaultShortcut: ShortcutChord | null;
  allowInEditable: boolean;
}

export function createShortcutChord(
  key: string,
  modifiers: Partial<ShortcutModifiers> = {},
): ShortcutChord {
  return {
    key,
    modifiers: {
      primary: false,
      control: false,
      meta: false,
      alt: false,
      shift: false,
      ...modifiers,
    },
  };
}

const primary = (key: string) => createShortcutChord(key, { primary: true });

export const SHORTCUT_ACTIONS: readonly ShortcutActionDefinition[] = [
  { id: 'openSearch', category: 'general', defaultShortcut: primary('K'), allowInEditable: true },
  {
    id: 'createIssue',
    category: 'general',
    defaultShortcut: createShortcutChord('C'),
    allowInEditable: false,
  },
  {
    id: 'toggleSidebar',
    category: 'general',
    defaultShortcut: primary('B'),
    allowInEditable: false,
  },
  { id: 'findInIssue', category: 'general', defaultShortcut: primary('F'), allowInEditable: true },
  { id: 'send', category: 'general', defaultShortcut: primary('Enter'), allowInEditable: true },
  { id: 'goInbox', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goChat', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goMyIssues', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goIssues', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goProjects', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goAutopilots', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goAgents', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goSquads', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goUsage', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goRuntimes', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goSkills', category: 'navigation', defaultShortcut: null, allowInEditable: false },
  { id: 'goSettings', category: 'navigation', defaultShortcut: null, allowInEditable: false },
] as const;

export const SHORTCUT_ACTION_BY_ID = Object.fromEntries(
  SHORTCUT_ACTIONS.map((action) => [action.id, action]),
) as Record<ShortcutActionId, ShortcutActionDefinition>;

const MODIFIER_KEYS = new Set([
  'Alt',
  'AltGraph',
  'CapsLock',
  'Control',
  'Fn',
  'FnLock',
  'Hyper',
  'Meta',
  'NumLock',
  'ScrollLock',
  'Shift',
  'Super',
  'Symbol',
  'SymbolLock',
]);
const NON_ACTIONABLE_KEYS = new Set(['Dead', 'Process', 'Unidentified']);
const KEY_LABELS: Record<string, string> = {
  ' ': 'Space',
  ArrowUp: 'Up',
  ArrowDown: 'Down',
  ArrowLeft: 'Left',
  ArrowRight: 'Right',
  Esc: 'Escape',
  '+': 'Plus',
  '-': 'Minus',
  '=': 'Equals',
  _: 'Underscore',
};

function eventKey(event: KeyboardEvent): string | null {
  if (typeof event.key !== 'string') return null;
  if (MODIFIER_KEYS.has(event.key) || NON_ACTIONABLE_KEYS.has(event.key)) return null;
  const key = KEY_LABELS[event.key] ?? event.key;
  if (key.length === 1 && /[a-z]/i.test(key)) return key.toUpperCase();
  return key;
}

export function isShortcutChordActionable(shortcut: ShortcutChord): boolean {
  return (
    shortcut.key.length > 0 &&
    !MODIFIER_KEYS.has(shortcut.key) &&
    !NON_ACTIONABLE_KEYS.has(shortcut.key)
  );
}

export function shortcutFromEvent(
  event: KeyboardEvent,
  platform: ShortcutPlatform = getShortcutPlatform(),
): ShortcutChord | null {
  if (event.getModifierState?.('AltGraph')) return null;
  const key = eventKey(event);
  if (!key) return null;
  const mac = platform === 'macos';
  return createShortcutChord(key, {
    primary: mac ? event.metaKey : event.ctrlKey,
    control: mac ? event.ctrlKey : false,
    meta: mac ? false : event.metaKey,
    alt: event.altKey,
    shift: event.shiftKey,
  });
}

export function shortcutChordEquals(
  left: ShortcutChord | null,
  right: ShortcutChord | null,
): boolean {
  if (left === null || right === null) return left === right;
  return (
    left.key === right.key &&
    left.modifiers.primary === right.modifiers.primary &&
    left.modifiers.control === right.modifiers.control &&
    left.modifiers.meta === right.modifiers.meta &&
    left.modifiers.alt === right.modifiers.alt &&
    left.modifiers.shift === right.modifiers.shift
  );
}

export function shortcutMatchesEvent(
  shortcut: ShortcutChord | null,
  event: KeyboardEvent,
  platform: ShortcutPlatform = getShortcutPlatform(),
): boolean {
  if (!shortcut) return false;
  const eventShortcut = shortcutFromEvent(event, platform);
  return eventShortcut !== null && shortcutChordEquals(shortcut, eventShortcut);
}

export function isPlainShortcut(shortcut: ShortcutChord | null, key: string): boolean {
  return shortcutChordEquals(shortcut, createShortcutChord(key));
}

export function formatShortcut(
  shortcut: ShortcutChord | null,
  platform: ShortcutPlatform = getShortcutPlatform(),
): string {
  if (!shortcut) return '—';
  const { modifiers } = shortcut;
  const keyLabels: Record<string, string> = {
    Enter: platform === 'macos' ? '↵' : 'Enter',
    Backspace: platform === 'macos' ? '⌫' : 'Backspace',
    Delete: platform === 'macos' ? '⌦' : 'Delete',
    Escape: 'Esc',
    Up: '↑',
    Down: '↓',
    Left: '←',
    Right: '→',
    Plus: '+',
    Minus: platform === 'macos' ? '−' : '-',
    Equals: '=',
    Underscore: '_',
    Space: 'Space',
  };
  const key = keyLabels[shortcut.key] ?? shortcut.key;

  if (platform === 'macos') {
    return [
      modifiers.primary ? '⌘' : '',
      modifiers.control ? '⌃' : '',
      modifiers.alt ? '⌥' : '',
      modifiers.shift ? '⇧' : '',
      modifiers.meta ? 'Meta' : '',
      key,
    ].join('');
  }

  return [
    modifiers.primary ? 'Ctrl' : null,
    modifiers.control ? 'Control' : null,
    modifiers.meta ? (platform === 'windows' ? 'Win' : 'Super') : null,
    modifiers.alt ? 'Alt' : null,
    modifiers.shift ? 'Shift' : null,
    key,
  ]
    .filter(Boolean)
    .join('+');
}

export function isEditableShortcutTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    target.tagName === 'INPUT' ||
    target.tagName === 'TEXTAREA' ||
    target.tagName === 'SELECT' ||
    target.closest("[contenteditable='true']") !== null
  );
}

const PRIMARY_RESERVED_KEYS = new Set([
  'W',
  'R',
  'Q',
  'A',
  'C',
  'V',
  'X',
  'Y',
  'Z',
  'Equals',
  'Plus',
  'Minus',
  'Underscore',
  '0',
]);

const BROWSER_ONLY_PRIMARY_RESERVED_KEYS = new Set(['P', 'L', 'T', 'N', 'D', 'U']);

export function isReservedShortcut(
  shortcut: ShortcutChord,
  platform: ShortcutPlatform = getShortcutPlatform(),
  runtime: ShortcutRuntime = getShortcutRuntime(),
): boolean {
  const { modifiers, key } = shortcut;
  if (key === 'F5') return true;
  if (modifiers.primary && PRIMARY_RESERVED_KEYS.has(key)) return true;
  if (modifiers.primary && BROWSER_ONLY_PRIMARY_RESERVED_KEYS.has(key)) {
    const barePrimary = !modifiers.control && !modifiers.meta && !modifiers.alt && !modifiers.shift;
    if (runtime !== 'desktop' || !barePrimary) return true;
  }

  if (platform === 'macos') {
    if (modifiers.primary && (key === 'Space' || key === 'Tab' || key === 'M' || key === 'H'))
      return true;
    if (modifiers.control && ['Up', 'Down', 'Left', 'Right'].includes(key)) return true;
  } else {
    if (modifiers.meta) return true;
    if (modifiers.alt && (key === 'Tab' || key === 'F4')) return true;
  }

  return false;
}

const PLAIN_GLOBAL_RESERVED_KEYS = new Set([
  'Enter',
  'Space',
  'Tab',
  'Escape',
  'Backspace',
  'Delete',
  'Up',
  'Down',
  'Left',
  'Right',
  'Home',
  'End',
  'PageUp',
  'PageDown',
]);

function hasShortcutModifier(shortcut: ShortcutChord): boolean {
  const { modifiers } = shortcut;
  return (
    modifiers.primary || modifiers.control || modifiers.meta || modifiers.alt || modifiers.shift
  );
}

function hasCommandModifier(shortcut: ShortcutChord): boolean {
  const { modifiers } = shortcut;
  return modifiers.primary || modifiers.control || modifiers.meta;
}

export function isShortcutAllowedForAction(
  actionId: ShortcutActionId,
  shortcut: ShortcutChord,
  platform: ShortcutPlatform = getShortcutPlatform(),
  runtime: ShortcutRuntime = getShortcutRuntime(),
): boolean {
  if (!isShortcutChordActionable(shortcut)) return false;
  if (isReservedShortcut(shortcut, platform, runtime)) return false;
  if (shortcut.key === 'Tab') return false;
  const hasModifier = hasShortcutModifier(shortcut);
  const hasCommand = hasCommandModifier(shortcut);

  if (actionId === 'send') {
    return (
      shortcutChordEquals(shortcut, createShortcutChord('Enter')) ||
      shortcutChordEquals(shortcut, createShortcutChord('Enter', { primary: true }))
    );
  }
  if (SHORTCUT_ACTION_BY_ID[actionId].allowInEditable) {
    return hasCommand;
  }
  return hasModifier || !PLAIN_GLOBAL_RESERVED_KEYS.has(shortcut.key);
}

export function parseLegacyShortcut(value: string): ShortcutChord | null {
  if (!value) return null;
  const parts = value.split('+');
  const key = parts.pop();
  if (!key) return null;
  const knownModifiers = new Set(['Mod', 'Ctrl', 'Meta', 'Alt', 'Shift']);
  if (parts.some((part) => !knownModifiers.has(part))) return null;
  return createShortcutChord(key, {
    primary: parts.includes('Mod'),
    control: parts.includes('Ctrl'),
    meta: parts.includes('Meta'),
    alt: parts.includes('Alt'),
    shift: parts.includes('Shift'),
  });
}
