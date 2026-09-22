import { z } from 'zod';

export interface JoinTarget {
  id: string;
  slug: string;
  name: string;
  description?: string;
  template_key?: string;
  member_count: number;
}

export const JoinTargetSchema = z.object({
  id: z.string().optional().default(''),
  slug: z.string().optional().default(''),
  name: z.string().optional().default(''),
  description: z.string().optional(),
  template_key: z.string().optional(),
  member_count: z.number().optional().default(0),
});

export const JoinTargetListSchema = z.array(JoinTargetSchema);

export const EMPTY_JOIN_TARGET_LIST: JoinTarget[] = [];

export interface JoinTargetResult {
  id: string;
  slug: string;
  name: string;
  already_member: boolean;
}

export const JoinTargetResultSchema = z.object({
  id: z.string().optional().default(''),
  slug: z.string().optional().default(''),
  name: z.string().optional().default(''),
  already_member: z.boolean().optional().default(false),
});

export const EMPTY_JOIN_TARGET_RESULT: JoinTargetResult = {
  id: '',
  slug: '',
  name: '',
  already_member: false,
};
