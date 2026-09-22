import { z } from 'zod';

export interface DeploymentUser {
  user_id: string;
  email: string;
  name: string;
  deactivated: boolean;
  deactivated_at: string | null;
  token_version: number;
  revoked_tokens: number;
  closed_connections: number;
}

export const DeploymentUserSchema = z.object({
  user_id: z.string().optional().default(''),
  email: z.string().optional().default(''),
  name: z.string().optional().default(''),
  deactivated: z.boolean().optional().default(false),
  deactivated_at: z
    .string()
    .nullish()
    .transform((v) => v ?? null),
  token_version: z.number().optional().default(0),
  revoked_tokens: z.number().optional().default(0),
  closed_connections: z.number().optional().default(0),
});

export const EMPTY_DEPLOYMENT_USER: DeploymentUser = {
  user_id: '',
  email: '',
  name: '',
  deactivated: false,
  deactivated_at: null,
  token_version: 0,
  revoked_tokens: 0,
  closed_connections: 0,
};
