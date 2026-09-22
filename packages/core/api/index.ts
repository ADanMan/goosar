export {
  ApiClient,
  ApiError,
  apiErrorCode,
  dispatchReasonCode,
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from './client';
export type { ApiClientOptions, ClientRuntimeSnapshot, ClientUsageRequest } from './client';
export { describeApiFailure, isTransportFailure } from './error-presentation';
export type { ApiFailure, ApiFailureKind } from './error-presentation';
export { parseStrict, parseWithFallback, SchemaMismatchError, setSchemaLogger } from './schema';
export type { ParseOptions } from './schema';
export {
  EMPTY_EXPORT_JOB,
  EMPTY_USER_ERASURE,
  ExportJobSchema,
  UserErasureSchema,
} from './data-export';
export type {
  ExportAttachmentSummary,
  ExportCounts,
  ExportJob,
  ExportManifest,
  UserErasure,
} from './data-export';
export {
  EMPTY_JOIN_TARGET_LIST,
  EMPTY_JOIN_TARGET_RESULT,
  JoinTargetListSchema,
  JoinTargetResultSchema,
  JoinTargetSchema,
} from './deployment-join';
export type { JoinTarget, JoinTargetResult } from './deployment-join';
export { DuplicateIssueErrorBodySchema } from './schemas';
export type { DuplicateIssueErrorBody } from './schemas';
export { AUTH_METHODS, AuthMethodsResponseSchema, EMPTY_AUTH_METHODS } from './schemas';
export type { AuthMethod, AuthMethodsResponse } from './schemas';
export { WSClient } from './ws-client';

import type { ApiClient as ApiClientType } from './client';

let _api: ApiClientType | null = null;

export function setApiInstance(instance: ApiClientType) {
  _api = instance;
}

export function getApi(): ApiClientType {
  if (!_api) throw new Error('ApiClient not initialised — call setApiInstance() first');
  return _api;
}

export const api = new Proxy({} as ApiClientType, {
  get(_target, prop, receiver) {
    if (!_api) return undefined;
    const value = Reflect.get(_api, prop, receiver);
    return typeof value === 'function' ? value.bind(_api) : value;
  },
});

export {
  DEFAULT_SESSION_POLICY,
  EMPTY_LOGIN_RESULT,
  EMPTY_MFA_CONFIRMATION,
  EMPTY_MFA_ENROLLMENT,
  EMPTY_MFA_STATUS,
  EMPTY_SESSION_LIST,
  EMPTY_SESSION_REVOKE,
  LoginResultSchema,
  MFAConfirmSchema,
  MFAEnrollSchema,
  MFAStatusSchema,
  REQUIRE_MFA_VALUES,
  SessionListSchema,
  SessionRevokeSchema,
  readSessionPolicy,
  writeSessionPolicy,
  type LoginResult,
  type MFAConfirmation,
  type MFAEnrollment,
  type MFAStatus,
  type RequireMFA,
  type SessionEntry,
  type SessionPolicy,
  type SessionRevokeResult,
} from './mfa';
