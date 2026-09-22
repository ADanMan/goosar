export type ProjectStatus = 'planned' | 'in_progress' | 'paused' | 'completed' | 'cancelled';

export type ProjectPriority = 'urgent' | 'high' | 'medium' | 'low' | 'none';

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead_type: 'member' | 'agent' | null;
  lead_id: string | null;
  start_date: string | null;
  due_date: string | null;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
  resource_count: number;
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: 'member' | 'agent';
  lead_id?: string;
  start_date?: string;
  due_date?: string;
  resources?: CreateProjectResourceRequest[];
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: 'member' | 'agent' | null;
  lead_id?: string | null;
  start_date?: string | null;
  due_date?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

export type ProjectResourceType = 'github_repo' | 'local_directory';

export interface GithubRepoResourceRef {
  url: string;
  ref?: string;
  default_branch_hint?: string;
}

export interface LocalDirectoryResourceRef {
  local_path: string;
  daemon_id: string;
  label?: string;
}

export type ProjectResourceRef =
  GithubRepoResourceRef | LocalDirectoryResourceRef | Record<string, unknown>;

export interface ProjectResource {
  id: string;
  project_id: string;
  workspace_id: string;
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label: string | null;
  position: number;
  created_at: string;
  created_by: string | null;
}

export interface CreateProjectResourceRequest {
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label?: string;
  position?: number;
}

export interface UpdateProjectResourceRequest {
  resource_ref?: ProjectResourceRef;
  label?: string | null;
  position?: number;
}

export interface ListProjectResourcesResponse {
  resources: ProjectResource[];
  total: number;
}
