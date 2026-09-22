import { z } from 'zod';

export interface WorkspaceContentConflict {
  path: string;
  reason: string;
}

export type WorkspaceContentSyncStatus = 'pending' | 'running' | 'completed' | 'failed' | 'timeout';

export interface WorkspaceContentSyncRequest {
  id: string;
  runtime_id: string;
  status: WorkspaceContentSyncStatus;
  written?: string[];
  unchanged?: string[];
  removed?: string[];
  released?: string[];
  conflicts?: WorkspaceContentConflict[];
  truncated?: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

export const WorkspaceContentConflictSchema = z.object({
  path: z.string(),
  reason: z.string(),
});

export const WorkspaceContentSyncRequestSchema = z.object({
  id: z.string(),
  runtime_id: z.string(),
  status: z.string(),
  written: z.array(z.string()).optional(),
  unchanged: z.array(z.string()).optional(),
  removed: z.array(z.string()).optional(),
  released: z.array(z.string()).optional(),
  conflicts: z.array(WorkspaceContentConflictSchema).optional(),
  truncated: z.boolean().optional(),
  error: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const EMPTY_WORKSPACE_CONTENT_SYNC_REQUEST: WorkspaceContentSyncRequest = {
  id: '',
  runtime_id: '',
  status: 'failed',
  error: 'workspace content sync response could not be read',
  created_at: '',
  updated_at: '',
};
