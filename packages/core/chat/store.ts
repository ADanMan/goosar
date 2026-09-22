import { create } from 'zustand';
import type { StorageAdapter } from '../types';
import type { Attachment } from '../types/attachment';
import { getCurrentSlug, registerForWorkspaceRehydration } from '../platform/workspace-storage';
import { registerDraftCleanup } from '../drafts/cleanup-registry';
import {
  normalizeStoredUploads,
  attachmentToDraftUpload,
  type DraftUpload,
  type PendingDraftUpload,
} from '../drafts/draft-upload';
import { createLogger } from '../logger';

const logger = createLogger('chat.store');

const AGENT_STORAGE_KEY = 'goosar:chat:selectedAgentId';
const PROJECT_STORAGE_KEY = 'goosar:chat:selectedProjectId';
const SESSION_STORAGE_KEY = 'goosar:chat:activeSessionId';
const DRAFTS_KEY = 'goosar:chat:drafts';
const DRAFT_ATTACHMENTS_KEY = 'goosar:chat:draft-attachments';
const APPLIED_RESTORES_KEY = 'goosar:chat:applied-draft-restores';
const PENDING_SEND_RESTORES_KEY = 'goosar:chat:pending-send-restores';
export const DRAFT_NEW_SESSION = '__new__';

const LEGACY_NEW_SESSION_PREFIX = `${DRAFT_NEW_SESSION}:`;
const CHAT_WIDTH_KEY = 'goosar:chat:width';
const CHAT_HEIGHT_KEY = 'goosar:chat:height';
const CHAT_EXPANDED_KEY = 'goosar:chat:expanded';
const OPEN_KEY = 'goosar:chat:isOpen';
const FLOATING_KEY = 'goosar:chat:floatingChatEnabled';

function readDrafts(storage: StorageAdapter, key: string): Record<string, string> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === 'object' && parsed !== null ? parsed : {};
  } catch {
    return {};
  }
}

function writeDrafts(storage: StorageAdapter, key: string, drafts: Record<string, string>) {
  const pruned: Record<string, string> = {};
  for (const [k, v] of Object.entries(drafts)) {
    if (v) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) {
    storage.removeItem(key);
  } else {
    storage.setItem(key, JSON.stringify(pruned));
  }
}

function isAttachmentDraft(value: unknown): value is Attachment {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { id?: unknown }).id === 'string' &&
    typeof (value as { filename?: unknown }).filename === 'string'
  );
}

function readDraftAttachments(storage: StorageAdapter, key: string): Record<string, DraftUpload[]> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return {};
    const out: Record<string, DraftUpload[]> = {};
    for (const [draftKey, value] of Object.entries(parsed)) {
      if (!Array.isArray(value)) continue;
      const uploads = normalizeStoredUploads(value);
      if (uploads.length > 0) out[draftKey] = uploads;
    }
    return out;
  } catch {
    return {};
  }
}

function readAppliedRestores(storage: StorageAdapter, key: string): string[] {
  const raw = storage.getItem(key);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((id): id is string => typeof id === 'string');
  } catch {
    return [];
  }
}

function writeAppliedRestores(storage: StorageAdapter, key: string, ids: string[]) {
  if (ids.length === 0) storage.removeItem(key);
  else storage.setItem(key, JSON.stringify(ids));
}

function isPendingSendRestore(value: unknown): value is PendingSendRestore {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as { id?: unknown; content?: unknown; sessionId?: unknown };
  return (
    typeof v.id === 'string' && typeof v.content === 'string' && typeof v.sessionId === 'string'
  );
}

function readPendingSendRestores(
  storage: StorageAdapter,
  key: string,
): Record<string, PendingSendRestore[]> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return {};
    const out: Record<string, PendingSendRestore[]> = {};
    for (const [sessionId, value] of Object.entries(parsed)) {
      if (!Array.isArray(value)) continue;
      const queued = value.filter(isPendingSendRestore).map((r) => ({
        ...r,
        attachments: Array.isArray(r.attachments) ? r.attachments.filter(isAttachmentDraft) : [],
      }));
      if (queued.length > 0) out[sessionId] = queued;
    }
    return out;
  } catch {
    return {};
  }
}

function writePendingSendRestores(
  storage: StorageAdapter,
  key: string,
  queues: Record<string, PendingSendRestore[]>,
) {
  const pruned: Record<string, PendingSendRestore[]> = {};
  for (const [k, v] of Object.entries(queues)) {
    if (v.length > 0) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) storage.removeItem(key);
  else storage.setItem(key, JSON.stringify(pruned));
}

function writeDraftAttachments(
  storage: StorageAdapter,
  key: string,
  drafts: Record<string, DraftUpload[]>,
) {
  const pruned: Record<string, DraftUpload[]> = {};
  for (const [k, v] of Object.entries(drafts)) {
    if (v.length > 0) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) {
    storage.removeItem(key);
  } else {
    storage.setItem(key, JSON.stringify(pruned));
  }
}

function migrateLegacyNewChatSlots<T>(
  slots: Record<string, T>,
  selectedAgentId: string | null,
): { slots: Record<string, T>; changed: boolean } {
  const legacyKeys = Object.keys(slots).filter((k) => k.startsWith(LEGACY_NEW_SESSION_PREFIX));
  if (legacyKeys.length === 0) return { slots, changed: false };
  const next = { ...slots };
  const adopted = next[`${LEGACY_NEW_SESSION_PREFIX}${selectedAgentId ?? ''}`];
  if (!(DRAFT_NEW_SESSION in next) && adopted !== undefined) {
    next[DRAFT_NEW_SESSION] = adopted;
  }
  for (const key of legacyKeys) delete next[key];
  logger.info('migrating legacy per-agent new-chat drafts', {
    legacyCount: legacyKeys.length,
    selectedAgentId,
    adopted: DRAFT_NEW_SESSION in next,
  });
  return { slots: next, changed: true };
}

function loadDraftSlots(
  storage: StorageAdapter,
  draftsKey: string,
  attachmentsKey: string,
  selectedAgentId: string | null,
): { inputDrafts: Record<string, string>; inputDraftAttachments: Record<string, DraftUpload[]> } {
  const drafts = migrateLegacyNewChatSlots(readDrafts(storage, draftsKey), selectedAgentId);
  const attachments = migrateLegacyNewChatSlots(
    readDraftAttachments(storage, attachmentsKey),
    selectedAgentId,
  );
  if (drafts.changed) writeDrafts(storage, draftsKey, drafts.slots);
  if (attachments.changed) writeDraftAttachments(storage, attachmentsKey, attachments.slots);
  return { inputDrafts: drafts.slots, inputDraftAttachments: attachments.slots };
}

export const CHAT_MIN_W = 360;
export const CHAT_MIN_H = 480;
export const CHAT_DEFAULT_W = 380;
export const CHAT_DEFAULT_H = 600;

export interface ChatTimelineItem {
  seq: number;
  type: 'tool_use' | 'tool_result' | 'thinking' | 'text' | 'error';
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

export interface PendingSendRestore {
  id: string;
  content: string;
  attachments?: Attachment[];
  sessionId: string;
}

export interface ChatState {
  isOpen: boolean;
  floatingChatEnabled: boolean;
  activeSessionId: string | null;
  selectedAgentId: string | null;
  selectedProjectId: string | null;
  inputDrafts: Record<string, string>;
  inputDraftAttachments: Record<string, DraftUpload[]>;
  appliedDraftRestoreIds: string[];
  pendingSendRestores: Record<string, PendingSendRestore[]>;
  chatWidth: number;
  chatHeight: number;
  isExpanded: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  setFloatingChatEnabled: (enabled: boolean) => void;
  setActiveSession: (id: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  setSelectedProjectId: (id: string | null) => void;
  setInputDraft: (sessionId: string, draft: string) => void;
  appendToInputDraft: (sessionId: string, markdown: string) => void;
  setInputDraftAttachments: (sessionId: string, uploads: DraftUpload[]) => void;
  addInputDraftAttachment: (sessionId: string, attachment: Attachment) => void;
  addInputDraftUpload: (sessionId: string, upload: DraftUpload) => void;
  settleInputDraftUpload: (
    sessionId: string,
    clientUploadId: string,
    attachment: Attachment,
  ) => void;
  failInputDraftUpload: (sessionId: string, clientUploadId: string, error?: string) => void;
  removeInputDraftUpload: (sessionId: string, clientUploadId: string) => void;
  clearInputDraft: (sessionId: string) => void;
  markDraftRestoreApplied: (restoreId: string) => void;
  forgetDraftRestoreApplied: (restoreId: string) => void;
  enqueuePendingSendRestore: (restore: PendingSendRestore) => void;
  dequeuePendingSendRestore: (sessionId: string, restoreId: string) => void;
  setChatSize: (width: number, height: number) => void;
  setExpanded: (expanded: boolean) => void;
}

export interface ChatStoreOptions {
  storage: StorageAdapter;
}

export function createChatStore(options: ChatStoreOptions) {
  const { storage } = options;

  const wsKey = (base: string) => {
    const slug = getCurrentSlug();
    return slug ? `${base}:${slug}` : base;
  };

  const storedOpen = storage.getItem(OPEN_KEY);
  const initialIsOpen = storedOpen === 'true';

  const initialFloatingEnabled = storage.getItem(FLOATING_KEY) !== 'false';

  const initialAgentId = storage.getItem(wsKey(AGENT_STORAGE_KEY));
  const initialDraftSlots = loadDraftSlots(
    storage,
    wsKey(DRAFTS_KEY),
    wsKey(DRAFT_ATTACHMENTS_KEY),
    initialAgentId,
  );

  const store = create<ChatState>((set, get) => ({
    isOpen: initialIsOpen,
    floatingChatEnabled: initialFloatingEnabled,
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    selectedAgentId: initialAgentId,
    selectedProjectId: storage.getItem(wsKey(PROJECT_STORAGE_KEY)),
    inputDrafts: initialDraftSlots.inputDrafts,
    inputDraftAttachments: initialDraftSlots.inputDraftAttachments,
    appliedDraftRestoreIds: readAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY)),
    pendingSendRestores: readPendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY)),
    chatWidth: Number(storage.getItem(CHAT_WIDTH_KEY)) || CHAT_DEFAULT_W,
    chatHeight: Number(storage.getItem(CHAT_HEIGHT_KEY)) || CHAT_DEFAULT_H,
    isExpanded: storage.getItem(wsKey(CHAT_EXPANDED_KEY)) === 'true',
    setOpen: (open) => {
      logger.debug('setOpen', { from: get().isOpen, to: open });
      storage.setItem(OPEN_KEY, String(open));
      set({ isOpen: open });
    },
    toggle: () => {
      const next = !get().isOpen;
      logger.debug('toggle', { to: next });
      storage.setItem(OPEN_KEY, String(next));
      set({ isOpen: next });
    },
    setFloatingChatEnabled: (enabled) => {
      logger.info('setFloatingChatEnabled', { to: enabled });
      storage.setItem(FLOATING_KEY, String(enabled));
      set(enabled ? { floatingChatEnabled: true } : { floatingChatEnabled: false, isOpen: false });
      if (!enabled) storage.setItem(OPEN_KEY, 'false');
    },
    setActiveSession: (id) => {
      logger.info('setActiveSession', { from: get().activeSessionId, to: id });
      if (id) {
        storage.setItem(wsKey(SESSION_STORAGE_KEY), id);
      } else {
        storage.removeItem(wsKey(SESSION_STORAGE_KEY));
      }
      set({ activeSessionId: id });
    },
    setSelectedAgentId: (id) => {
      logger.info('setSelectedAgentId', { from: get().selectedAgentId, to: id });
      storage.setItem(wsKey(AGENT_STORAGE_KEY), id);
      set({ selectedAgentId: id });
    },
    setSelectedProjectId: (id) => {
      logger.info('setSelectedProjectId', { from: get().selectedProjectId, to: id });
      if (id) storage.setItem(wsKey(PROJECT_STORAGE_KEY), id);
      else storage.removeItem(wsKey(PROJECT_STORAGE_KEY));
      set({ selectedProjectId: id });
    },
    markDraftRestoreApplied: (restoreId) => {
      const current = get().appliedDraftRestoreIds;
      if (current.includes(restoreId)) return;
      const next = [...current, restoreId];
      writeAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY), next);
      set({ appliedDraftRestoreIds: next });
    },
    forgetDraftRestoreApplied: (restoreId) => {
      const current = get().appliedDraftRestoreIds;
      if (!current.includes(restoreId)) return;
      const next = current.filter((id) => id !== restoreId);
      writeAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY), next);
      set({ appliedDraftRestoreIds: next });
    },
    enqueuePendingSendRestore: (restore) => {
      if (!restore.sessionId || !restore.id) return;
      const current = get().pendingSendRestores;
      const existing = current[restore.sessionId] ?? [];
      if (existing.some((r) => r.id === restore.id)) return;
      logger.info('enqueuePendingSendRestore', {
        sessionId: restore.sessionId,
        restoreId: restore.id,
      });
      const next = { ...current, [restore.sessionId]: [...existing, restore] };
      writePendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY), next);
      set({ pendingSendRestores: next });
    },
    dequeuePendingSendRestore: (sessionId, restoreId) => {
      const current = get().pendingSendRestores;
      const existing = current[sessionId];
      if (!existing?.some((r) => r.id === restoreId)) return;
      logger.info('dequeuePendingSendRestore', { sessionId, restoreId });
      const remaining = existing.filter((r) => r.id !== restoreId);
      const next = { ...current };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      writePendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY), next);
      set({ pendingSendRestores: next });
    },
    setInputDraft: (sessionId, draft) => {
      logger.debug('setInputDraft', { sessionId, length: draft.length });
      const next = { ...get().inputDrafts, [sessionId]: draft };
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
    appendToInputDraft: (sessionId, markdown) => {
      const existing = get().inputDrafts[sessionId] ?? '';
      const draft = existing.trim() ? `${existing.replace(/\s+$/, '')}\n\n${markdown}` : markdown;
      logger.debug('appendToInputDraft', { sessionId, length: draft.length });
      const next = { ...get().inputDrafts, [sessionId]: draft };
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
    setInputDraftAttachments: (sessionId, uploads) => {
      logger.debug('setInputDraftAttachments', { sessionId, count: uploads.length });
      const next = { ...get().inputDraftAttachments };
      if (uploads.length > 0) next[sessionId] = uploads;
      else delete next[sessionId];
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    addInputDraftAttachment: (sessionId, attachment) => {
      if (!attachment.id) return;
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      const wrapped = attachmentToDraftUpload(attachment);
      const nextForKey = existing.some(
        (u) => u.status === 'uploaded' && u.attachment.id === attachment.id,
      )
        ? existing.map((u) =>
            u.status === 'uploaded' && u.attachment.id === attachment.id ? wrapped : u,
          )
        : [...existing, wrapped];
      const next = { ...current, [sessionId]: nextForKey };
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    addInputDraftUpload: (sessionId, upload) => {
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      if (existing.some((u) => u.clientUploadId === upload.clientUploadId)) return;
      const next = { ...current, [sessionId]: [...existing, upload] };
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    settleInputDraftUpload: (sessionId, clientUploadId, attachment) => {
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      if (!existing.some((u) => u.clientUploadId === clientUploadId)) return;
      const nextForKey = existing.map((u) =>
        u.clientUploadId === clientUploadId
          ? { ...attachmentToDraftUpload(attachment), clientUploadId }
          : u,
      );
      const next = { ...current, [sessionId]: nextForKey };
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    failInputDraftUpload: (sessionId, clientUploadId, error) => {
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      const target = existing.find((u) => u.clientUploadId === clientUploadId);
      if (!target) return;
      const failed: PendingDraftUpload = {
        clientUploadId,
        status: 'failed',
        filename: target.filename,
        size: target.size,
        contentType: target.contentType,
        error,
      };
      const nextForKey = existing.map((u) => (u.clientUploadId === clientUploadId ? failed : u));
      const next = { ...current, [sessionId]: nextForKey };
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    removeInputDraftUpload: (sessionId, clientUploadId) => {
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      if (!existing.some((u) => u.clientUploadId === clientUploadId)) return;
      const remaining = existing.filter((u) => u.clientUploadId !== clientUploadId);
      const next = { ...current };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    clearInputDraft: (sessionId) => {
      const currentDrafts = get().inputDrafts;
      const currentAttachments = get().inputDraftAttachments;
      if (!(sessionId in currentDrafts) && !(sessionId in currentAttachments)) {
        logger.debug('clearInputDraft skipped (no draft)', { sessionId });
        return;
      }
      logger.info('clearInputDraft', { sessionId });
      const nextDrafts = { ...currentDrafts };
      const nextAttachments = { ...currentAttachments };
      delete nextDrafts[sessionId];
      delete nextAttachments[sessionId];
      writeDrafts(storage, wsKey(DRAFTS_KEY), nextDrafts);
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), nextAttachments);
      set({ inputDrafts: nextDrafts, inputDraftAttachments: nextAttachments });
    },
    setChatSize: (w, h) => {
      logger.debug('setChatSize', { w, h });
      storage.setItem(CHAT_WIDTH_KEY, String(w));
      storage.setItem(CHAT_HEIGHT_KEY, String(h));
      storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      set({ chatWidth: w, chatHeight: h, isExpanded: false });
    },
    setExpanded: (expanded) => {
      logger.info('setExpanded', { to: expanded });
      if (expanded) {
        storage.setItem(wsKey(CHAT_EXPANDED_KEY), 'true');
      } else {
        storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      }
      set({ isExpanded: expanded });
    },
  }));

  registerDraftCleanup({
    storageKey: DRAFTS_KEY,
    workspaceScoped: true,
    resetInMemory: () => store.setState({ inputDrafts: {} }),
  });
  registerDraftCleanup({
    storageKey: DRAFT_ATTACHMENTS_KEY,
    workspaceScoped: true,
    resetInMemory: () => store.setState({ inputDraftAttachments: {} }),
  });
  registerDraftCleanup({
    storageKey: APPLIED_RESTORES_KEY,
    workspaceScoped: true,
    resetInMemory: () => store.setState({ appliedDraftRestoreIds: [] }),
  });
  registerDraftCleanup({
    storageKey: PENDING_SEND_RESTORES_KEY,
    workspaceScoped: true,
    resetInMemory: () => store.setState({ pendingSendRestores: {} }),
  });

  registerForWorkspaceRehydration(() => {
    const nextSession = storage.getItem(wsKey(SESSION_STORAGE_KEY));
    const nextAgent = storage.getItem(wsKey(AGENT_STORAGE_KEY));
    const nextProject = storage.getItem(wsKey(PROJECT_STORAGE_KEY));
    const { inputDrafts: nextDrafts, inputDraftAttachments: nextDraftAttachments } = loadDraftSlots(
      storage,
      wsKey(DRAFTS_KEY),
      wsKey(DRAFT_ATTACHMENTS_KEY),
      nextAgent,
    );
    logger.info('workspace rehydration', {
      prevSession: store.getState().activeSessionId,
      nextSession,
      prevAgent: store.getState().selectedAgentId,
      nextAgent,
      prevProject: store.getState().selectedProjectId,
      nextProject,
      draftCount: Object.keys(nextDrafts).length,
      draftAttachmentCount: Object.keys(nextDraftAttachments).length,
    });
    store.setState({
      activeSessionId: nextSession,
      selectedAgentId: nextAgent,
      selectedProjectId: nextProject,
      inputDrafts: nextDrafts,
      inputDraftAttachments: nextDraftAttachments,
      appliedDraftRestoreIds: readAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY)),
      pendingSendRestores: readPendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY)),
    });
  });

  return store;
}
