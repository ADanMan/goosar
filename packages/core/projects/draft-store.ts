import type { ProjectStatus, ProjectPriority } from '../types';
import { createDraftStore } from '../drafts/create-draft-store';

interface ProjectDraft {
  title: string;
  description: string;
  status: ProjectStatus;
  priority: ProjectPriority;
  leadType?: 'member' | 'agent';
  leadId?: string;
  icon?: string;
  startDate?: string;
  dueDate?: string;
}

const EMPTY_DRAFT: ProjectDraft = {
  title: '',
  description: '',
  status: 'planned',
  priority: 'none',
  leadType: undefined,
  leadId: undefined,
  icon: undefined,
  startDate: undefined,
  dueDate: undefined,
};

export const useProjectDraftStore = createDraftStore<ProjectDraft>({
  storageKey: 'goosar_project_draft',
  emptyData: EMPTY_DRAFT,
  hasMeaningful: (d) => !!(d.title || d.description),
});
