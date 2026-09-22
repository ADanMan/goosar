import { z } from 'zod';
import { UserSchema, EMPTY_USER } from './schemas';
import type { User } from '../types';

export const LoginResultSchema = z
  .object({
    token: z.string().optional().default(''),
    user: UserSchema.optional(),
    mfa_required: z.boolean().optional().default(false),
    mfa_token: z.string().optional().default(''),
    mfa_enrollment_required: z.boolean().optional().default(false),
  })
  .loose();

export interface LoginResult {
  token: string;
  user?: User;
  mfa_required: boolean;
  mfa_token: string;
  mfa_enrollment_required: boolean;
}

export const EMPTY_LOGIN_RESULT: LoginResult = {
  token: '',
  user: EMPTY_USER,
  mfa_required: false,
  mfa_token: '',
  mfa_enrollment_required: false,
};

export const MFAStatusSchema = z
  .object({
    enabled: z.boolean().optional().default(false),
    pending_enrollment: z.boolean().optional().default(false),
    enabled_at: z.string().nullish(),
    recovery_codes_remaining: z.number().optional().default(0),
    required: z.boolean().optional().default(false),
    available: z.boolean().optional().default(false),
  })
  .loose();

export interface MFAStatus {
  enabled: boolean;
  pending_enrollment: boolean;
  enabled_at?: string | null;
  recovery_codes_remaining: number;
  required: boolean;
  available: boolean;
}

export const EMPTY_MFA_STATUS: MFAStatus = {
  enabled: false,
  pending_enrollment: false,
  enabled_at: null,
  recovery_codes_remaining: 0,
  required: false,
  available: false,
};

export const MFAEnrollSchema = z
  .object({
    secret: z.string().optional().default(''),
    otpauth_uri: z.string().optional().default(''),
    issuer: z.string().optional().default(''),
    account: z.string().optional().default(''),
    digits: z.number().optional().default(6),
    period_seconds: z.number().optional().default(30),
    algorithm: z.string().optional().default('SHA1'),
  })
  .loose();

export interface MFAEnrollment {
  secret: string;
  otpauth_uri: string;
  issuer: string;
  account: string;
  digits: number;
  period_seconds: number;
  algorithm: string;
}

export const EMPTY_MFA_ENROLLMENT: MFAEnrollment = {
  secret: '',
  otpauth_uri: '',
  issuer: '',
  account: '',
  digits: 6,
  period_seconds: 30,
  algorithm: 'SHA1',
};

export const MFAConfirmSchema = z
  .object({
    enabled: z.boolean().optional().default(false),
    recovery_codes: z.array(z.string()).optional().default([]),
  })
  .loose();

export interface MFAConfirmation {
  enabled: boolean;
  recovery_codes: string[];
}

export const EMPTY_MFA_CONFIRMATION: MFAConfirmation = {
  enabled: false,
  recovery_codes: [],
};

export const SessionSchema = z
  .object({
    id: z.string().optional().default(''),
    user_agent: z.string().optional().default(''),
    created_at: z.string().optional().default(''),
    last_seen_at: z.string().optional().default(''),
    current: z.boolean().optional().default(false),
  })
  .loose();

export const SessionListSchema = z.array(SessionSchema);

export interface SessionEntry {
  id: string;
  user_agent: string;
  created_at: string;
  last_seen_at: string;
  current: boolean;
}

export const EMPTY_SESSION_LIST: SessionEntry[] = [];

export const SessionRevokeSchema = z
  .object({
    revoked: z.number().optional().default(0),
    token_version: z.number().optional().default(0),
  })
  .loose();

export interface SessionRevokeResult {
  revoked: number;
  token_version: number;
}

export const EMPTY_SESSION_REVOKE: SessionRevokeResult = {
  revoked: 0,
  token_version: 0,
};

export const REQUIRE_MFA_VALUES = ['none', 'admins', 'all'] as const;
export type RequireMFA = (typeof REQUIRE_MFA_VALUES)[number];

export interface SessionPolicy {
  idle_timeout_hours: number;
  absolute_lifetime_days: number;
  max_concurrent_sessions: number;
  require_mfa: RequireMFA;
}

export const DEFAULT_SESSION_POLICY: SessionPolicy = {
  idle_timeout_hours: 12,
  absolute_lifetime_days: 30,
  max_concurrent_sessions: 0,
  require_mfa: 'none',
};

export function readSessionPolicy(policy: unknown): SessionPolicy {
  const out = { ...DEFAULT_SESSION_POLICY };
  if (typeof policy !== 'object' || policy === null) return out;
  const block = (policy as Record<string, unknown>).session;
  if (typeof block !== 'object' || block === null) return out;
  const raw = block as Record<string, unknown>;
  if (typeof raw.idle_timeout_hours === 'number') {
    out.idle_timeout_hours = raw.idle_timeout_hours;
  }
  if (typeof raw.absolute_lifetime_days === 'number') {
    out.absolute_lifetime_days = raw.absolute_lifetime_days;
  }
  if (typeof raw.max_concurrent_sessions === 'number') {
    out.max_concurrent_sessions = raw.max_concurrent_sessions;
  }
  if (
    typeof raw.require_mfa === 'string' &&
    (REQUIRE_MFA_VALUES as readonly string[]).includes(raw.require_mfa)
  ) {
    out.require_mfa = raw.require_mfa as RequireMFA;
  }
  return out;
}

export function writeSessionPolicy(
  policy: unknown,
  session: SessionPolicy,
): Record<string, unknown> {
  const base =
    typeof policy === 'object' && policy !== null ? { ...(policy as Record<string, unknown>) } : {};
  base.session = { ...session };
  return base;
}
