import { z } from 'zod';

export interface ExportCounts {
  [entity: string]: number;
}

export interface ExportAttachmentSummary {
  count: number;
  bytes: number;
  skipped: string[];
}

export interface ExportManifest {
  schema_version: number;
  kind: string;
  workspace_id: string;
  user_id: string;
  generated_at: string;
  counts: ExportCounts;
  attachments: ExportAttachmentSummary;
  redacted: string[];
  truncated: boolean;
  notes: string[];
}

export interface ExportJob {
  id: string;
  workspace_id: string;
  status: string;
  error: string | null;
  size_bytes: number;
  created_at: string;
  completed_at: string | null;
  manifest: ExportManifest | null;
  download_url: string | null;
}

const ExportManifestSchema = z.object({
  schema_version: z.number().optional().default(0),
  kind: z.string().optional().default(''),
  workspace_id: z.string().optional().default(''),
  user_id: z.string().optional().default(''),
  generated_at: z.string().optional().default(''),
  counts: z.record(z.string(), z.number()).optional().default({}),
  attachments: z
    .object({
      count: z.number().optional().default(0),
      bytes: z.number().optional().default(0),
      skipped: z.array(z.string()).optional().default([]),
    })
    .optional()
    .default({ count: 0, bytes: 0, skipped: [] }),
  redacted: z.array(z.string()).optional().default([]),
  truncated: z.boolean().optional().default(false),
  notes: z.array(z.string()).optional().default([]),
});

export const ExportJobSchema = z.object({
  id: z.string().optional().default(''),
  workspace_id: z.string().optional().default(''),
  status: z.string().optional().default(''),
  error: z
    .string()
    .nullish()
    .transform((v) => v ?? null),
  size_bytes: z.number().optional().default(0),
  created_at: z.string().optional().default(''),
  completed_at: z
    .string()
    .nullish()
    .transform((v) => v ?? null),
  manifest: ExportManifestSchema.nullish()
    .catch(null)
    .transform((v) => v ?? null),
  download_url: z
    .string()
    .nullish()
    .transform((v) => v ?? null),
});

export const EMPTY_EXPORT_JOB: ExportJob = {
  id: '',
  workspace_id: '',
  status: '',
  error: null,
  size_bytes: 0,
  created_at: '',
  completed_at: null,
  manifest: null,
  download_url: null,
};

export interface UserErasure {
  user_id: string;
  email: string;
  name: string;
  removed_memberships: number;
  revoked_tokens: number;
  closed_connections: number;
}

export const UserErasureSchema = z.object({
  user_id: z.string().optional().default(''),
  email: z.string().optional().default(''),
  name: z.string().optional().default(''),
  removed_memberships: z.number().optional().default(0),
  revoked_tokens: z.number().optional().default(0),
  closed_connections: z.number().optional().default(0),
});

export const EMPTY_USER_ERASURE: UserErasure = {
  user_id: '',
  email: '',
  name: '',
  removed_memberships: 0,
  revoked_tokens: 0,
  closed_connections: 0,
};
