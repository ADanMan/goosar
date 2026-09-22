import { apiErrorCode, describeApiFailure } from '@goosar/core/api';
import type { useT } from '../i18n';

type CommonTranslate = ReturnType<typeof useT<'common'>>['t'];

export function serverErrorMessage(t: CommonTranslate, code: string | undefined): string | null {
  switch (code) {
    case 'email_not_configured':
      return t(($) => $.server_errors.email_not_configured);
    case 'signup_disabled':
      return t(($) => $.server_errors.signup_disabled);
    case 'email_domain_not_allowed':
      return t(($) => $.server_errors.email_domain_not_allowed);
    case 'code_rate_limited':
      return t(($) => $.server_errors.code_rate_limited);
    case 'invalid_or_expired_code':
      return t(($) => $.server_errors.invalid_or_expired_code);
    case 'account_deactivated':
      return t(($) => $.server_errors.account_deactivated);
    case 'auth_method_disabled':
      return t(($) => $.server_errors.auth_method_disabled);
    case 'oidc_not_configured':
      return t(($) => $.server_errors.oidc_not_configured);
    case 'oidc_provider_unavailable':
      return t(($) => $.server_errors.oidc_provider_unavailable);
    case 'oidc_provider_refused':
      return t(($) => $.server_errors.oidc_provider_refused);
    case 'oidc_state_invalid':
      return t(($) => $.server_errors.oidc_state_invalid);
    case 'oidc_token_invalid':
      return t(($) => $.server_errors.oidc_token_invalid);
    case 'ldap_not_configured':
      return t(($) => $.server_errors.ldap_not_configured);
    case 'ldap_unavailable':
      return t(($) => $.server_errors.ldap_unavailable);
    case 'ldap_invalid_credentials':
      return t(($) => $.server_errors.ldap_invalid_credentials);
    case 'corporate_email_missing':
      return t(($) => $.server_errors.corporate_email_missing);
    case 'corporate_email_unverified':
      return t(($) => $.server_errors.corporate_email_unverified);
    case 'corporate_login_failed':
      return t(($) => $.server_errors.corporate_login_failed);
    case 'mfa_pending_invalid':
      return t(($) => $.server_errors.mfa_pending_invalid);
    case 'mfa_invalid_code':
      return t(($) => $.server_errors.mfa_invalid_code);
    case 'mfa_code_replayed':
      return t(($) => $.server_errors.mfa_code_replayed);
    case 'mfa_not_enrolled':
      return t(($) => $.server_errors.mfa_not_enrolled);
    case 'mfa_already_enrolled':
      return t(($) => $.server_errors.mfa_already_enrolled);
    case 'mfa_unavailable':
      return t(($) => $.server_errors.mfa_unavailable);
    case 'invitation_not_found':
      return t(($) => $.server_errors.invitation_not_found);
    case 'invitation_not_yours':
      return t(($) => $.server_errors.invitation_not_yours);
    case 'invitation_expired':
      return t(($) => $.server_errors.invitation_expired);
    case 'invitation_not_pending':
      return t(($) => $.server_errors.invitation_not_pending);
    case 'invitation_already_pending':
      return t(($) => $.server_errors.invitation_already_pending);
    case 'already_member':
      return t(($) => $.server_errors.already_member);
    case 'already_member_self':
      return t(($) => $.server_errors.already_member_self);
    case 'workspace_creation_disabled':
      return t(($) => $.server_errors.workspace_creation_disabled);
    case 'workspace_slug_taken':
      return t(($) => $.server_errors.workspace_slug_taken);
    case 'workspace_slug_reserved':
      return t(($) => $.server_errors.workspace_slug_reserved);
    case 'workspace_slug_invalid':
      return t(($) => $.server_errors.workspace_slug_invalid);
    case 'insufficient_permissions':
      return t(($) => $.server_errors.insufficient_permissions);
    default:
      return null;
  }
}

export interface ServerFailureText {
  text: string;
  detail?: string;
}

export function describeServerFailure(
  t: CommonTranslate,
  err: unknown,
  fallback: string,
): ServerFailureText {
  const described = describeApiFailure(err);
  const localized = serverErrorMessage(t, apiErrorCode(err));
  const text = localized ?? described.message ?? fallback;
  return {
    text,
    detail: described.detail === text ? undefined : described.detail,
  };
}
