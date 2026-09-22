import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type {
  IssueStatus,
  IssuePriority,
  IssueAssigneeType,
  IssuePropertyValues,
} from '../../types';
import type { CreateMode } from './create-mode-store';
import type { QuickCreateActorType } from './quick-create-store';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';
import { registerDraftCleanup } from '../../drafts/cleanup-registry';
import { normalizeStoredUploads, type DraftUpload } from '../../drafts/draft-upload';

export interface IssueCreateShared {
  projectId?: string;
  priority: IssuePriority;
  dueDate: string | null;
  attachments: DraftUpload[];
}

export interface IssueCreateManual {
  title: string;
  description: string;
  status: IssueStatus;
  startDate: string | null;
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  labelIds: string[];
  propertyValues: IssuePropertyValues;
}

export interface IssueCreateAgent {
  prompt: string;
  actorType?: QuickCreateActorType;
  actorId?: string;
}

export interface IssueCreateDraft {
  shared: IssueCreateShared;
  manual: IssueCreateManual;
  agent: IssueCreateAgent;
  activeMode: CreateMode;
}

const emptyShared = (): IssueCreateShared => ({
  projectId: undefined,
  priority: 'none',
  dueDate: null,
  attachments: [],
});

const emptyManual = (): IssueCreateManual => ({
  title: '',
  description: '',
  status: 'todo',
  startDate: null,
  assigneeType: undefined,
  assigneeId: undefined,
  labelIds: [],
  propertyValues: {},
});

const emptyAgent = (): IssueCreateAgent => ({
  prompt: '',
  actorType: undefined,
  actorId: undefined,
});

interface IssueDraftStore {
  draft: IssueCreateDraft;
  lastAssigneeType?: IssueAssigneeType;
  lastAssigneeId?: string;
  setShared: (patch: Partial<IssueCreateShared>) => void;
  setManual: (patch: Partial<IssueCreateManual>) => void;
  setAgent: (patch: Partial<IssueCreateAgent>) => void;
  setActiveMode: (mode: CreateMode) => void;
  clearDraft: () => void;
  setLastAssignee: (type?: IssueAssigneeType, id?: string) => void;
  hasDraft: () => boolean;
}

function isLegacyFlatDraft(d: Record<string, unknown>): boolean {
  return (
    !('manual' in d) &&
    !('shared' in d) &&
    ('title' in d || 'status' in d || 'labelIds' in d || 'description' in d)
  );
}

function migrateDraft(raw: unknown): IssueCreateDraft {
  const d = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;

  if (isLegacyFlatDraft(d)) {
    return {
      shared: {
        ...emptyShared(),
        projectId: d.projectId as string | undefined,
        priority: (d.priority as IssuePriority) ?? 'none',
        dueDate: (d.dueDate as string | null) ?? null,
        attachments: normalizeStoredUploads(d.attachments),
      },
      manual: {
        ...emptyManual(),
        title: (d.title as string) ?? '',
        description: (d.description as string) ?? '',
        status: (d.status as IssueStatus) ?? 'todo',
        startDate: (d.startDate as string | null) ?? null,
        assigneeType: d.assigneeType as IssueAssigneeType | undefined,
        assigneeId: d.assigneeId as string | undefined,
        labelIds: Array.isArray(d.labelIds) ? (d.labelIds as string[]) : [],
        propertyValues:
          d.propertyValues && typeof d.propertyValues === 'object'
            ? (d.propertyValues as IssuePropertyValues)
            : {},
      },
      agent: emptyAgent(),
      activeMode: 'manual',
    };
  }

  const sharedRaw = (d.shared as Partial<IssueCreateShared> & { attachments?: unknown }) ?? {};
  return {
    shared: {
      ...emptyShared(),
      ...sharedRaw,
      attachments: normalizeStoredUploads(sharedRaw.attachments),
    },
    manual: { ...emptyManual(), ...((d.manual as Partial<IssueCreateManual>) ?? {}) },
    agent: { ...emptyAgent(), ...((d.agent as Partial<IssueCreateAgent>) ?? {}) },
    activeMode: d.activeMode === 'agent' ? 'agent' : 'manual',
  };
}

export const useIssueDraftStore = create<IssueDraftStore>()(
  persist(
    (set, get) => ({
      draft: migrateDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
      setShared: (patch) =>
        set((s) => ({ draft: { ...s.draft, shared: { ...s.draft.shared, ...patch } } })),
      setManual: (patch) =>
        set((s) => ({ draft: { ...s.draft, manual: { ...s.draft.manual, ...patch } } })),
      setAgent: (patch) =>
        set((s) => ({ draft: { ...s.draft, agent: { ...s.draft.agent, ...patch } } })),
      setActiveMode: (mode) => set((s) => ({ draft: { ...s.draft, activeMode: mode } })),
      clearDraft: () =>
        set((s) => ({
          draft: {
            shared: emptyShared(),
            manual: {
              ...emptyManual(),
              assigneeType: s.lastAssigneeType,
              assigneeId: s.lastAssigneeId,
            },
            agent: emptyAgent(),
            activeMode: s.draft.activeMode,
          },
        })),
      setLastAssignee: (type, id) => set({ lastAssigneeType: type, lastAssigneeId: id }),
      hasDraft: () => {
        const { manual, agent, shared } = get().draft;
        return !!(
          manual.title ||
          manual.description ||
          agent.prompt ||
          Object.keys(manual.propertyValues).length > 0 ||
          shared.attachments.some((u) => u.status === 'uploaded' || u.status === 'uploading')
        );
      },
    }),
    {
      name: 'goosar_issue_draft',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<IssueDraftStore> & {
          draft?: unknown;
        };
        return {
          ...currentState,
          ...persisted,
          draft: migrateDraft(persisted.draft),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueDraftStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: 'goosar_issue_draft',
  workspaceScoped: true,
  resetInMemory: () =>
    useIssueDraftStore.setState({
      draft: migrateDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
    }),
});
