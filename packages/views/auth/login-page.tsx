'use client';

import { useState, useEffect, useCallback, useRef, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from '@goosar/ui/components/ui/card';
import { Input } from '@goosar/ui/components/ui/input';
import { Button } from '@goosar/ui/components/ui/button';
import { Label } from '@goosar/ui/components/ui/label';
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@goosar/ui/components/ui/input-otp';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@goosar/ui/components/ui/collapsible';
import { useAuthStore } from '@goosar/core/auth';
import { useConfigStore } from '@goosar/core/config';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import { api, isTransportFailure } from '@goosar/core/api';
import { describeServerFailure, serverErrorMessage } from '../common/server-error';
import {
  CorporateSignInButton,
  LdapSignInForm,
  hasMethod,
  useAuthMethods,
} from './corporate-signin';
import type { User } from '@goosar/core/types';
import { useT } from '../i18n';

interface CliCallbackConfig {
  url: string;
  state: string;
}

interface LoginPageProps {
  logo?: ReactNode;
  onSuccess: () => void;
  cliCallback?: CliCallbackConfig;
  onTokenObtained?: () => void;
  extra?: ReactNode;
  describeError?: (err: unknown) => string | undefined;
  onCorporateLogin?: (startUrl: string) => void;
  corporateClient?: 'desktop';
  corporateErrorCode?: string | null;
  corporateMfaToken?: string | null;
  onError?: (err: unknown) => void;
}

interface LoginError {
  text: string;
  detail?: string;
}

type LoginFailure =
  { kind: 'message'; text: string } | { kind: 'request'; err: unknown; fallback: string };

export function redirectToCliCallback(url: string, token: string, state: string) {
  const separator = url.includes('?') ? '&' : '?';
  window.location.href = `${url}${separator}token=${encodeURIComponent(token)}&state=${encodeURIComponent(state)}`;
}

export function validateCliCallback(cliCallback: string): boolean {
  try {
    const cbUrl = new URL(cliCallback);
    if (cbUrl.protocol !== 'http:') return false;
    const h = cbUrl.hostname;
    if (h === 'localhost' || h === '127.0.0.1') return true;
    if (/^10\./.test(h)) return true;
    if (/^172\.(1[6-9]|2\d|3[01])\./.test(h)) return true;
    if (/^192\.168\./.test(h)) return true;
    return false;
  } catch {
    return false;
  }
}

function LoginErrorNote({ error }: { error: LoginError | null }) {
  const { t } = useT('auth');
  const [open, setOpen] = useState(false);

  if (!error) return null;

  return (
    <div className="w-full space-y-1 text-left" role="alert">
      <p className="text-sm break-words text-destructive">{error.text}</p>
      {error.detail && (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger className="text-xs text-muted-foreground underline-offset-4 hover:underline">
            {t(($) => $.errors.details)}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-1 max-h-32 overflow-auto rounded bg-muted/40 p-2 text-xs whitespace-pre-wrap break-all text-muted-foreground">
              {error.detail}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
}

function MailTransportNotice() {
  const { t } = useT('auth');
  const transport = useConfigStore((state) => state.emailTransport);

  if (transport !== 'dev' && transport !== 'none') return null;

  return (
    <p
      className="w-full rounded-md bg-muted/50 p-2 text-left text-xs text-muted-foreground"
      role="status"
    >
      {transport === 'dev'
        ? t(($) => $.signin.dev_transport_notice)
        : t(($) => $.errors.email_not_configured)}
    </p>
  );
}

export function LoginPage({
  logo,
  onSuccess,
  cliCallback,
  onTokenObtained,
  extra,
  describeError,
  onError,
  onCorporateLogin,
  corporateClient,
  corporateErrorCode,
  corporateMfaToken,
}: LoginPageProps) {
  const { t } = useT('auth');
  const { t: tCommon } = useT('common');
  const qc = useQueryClient();
  const [step, setStep] = useState<'email' | 'code' | 'mfa' | 'cli_confirm'>('email');
  const [mfaToken, setMfaToken] = useState('');
  const [useRecoveryCode, setUseRecoveryCode] = useState(false);
  const [mfaCode, setMfaCode] = useState('');
  const authMethods = useAuthMethods();
  const [door, setDoor] = useState<'email' | 'ldap'>('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [failure, setFailure] = useState<LoginFailure | null>(null);
  const [loading, setLoading] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [existingUser, setExistingUser] = useState<User | null>(null);
  const authSourceRef = useRef<'cookie' | 'localStorage'>('cookie');

  useEffect(() => {
    if (!cliCallback) return;

    api.setToken(null);

    api
      .getMe()
      .then((user) => {
        authSourceRef.current = 'cookie';
        setExistingUser(user);
        setStep('cli_confirm');
      })
      .catch(() => {
        const token = localStorage.getItem('goosar_token');
        if (!token) return;

        api.setToken(token);
        api
          .getMe()
          .then((user) => {
            authSourceRef.current = 'localStorage';
            setExistingUser(user);
            setStep('cli_confirm');
          })
          .catch(() => {
            api.setToken(null);
            localStorage.removeItem('goosar_token');
          });
      });
  }, [cliCallback]);

  useEffect(() => {
    const ticket = corporateMfaToken?.trim();
    if (!ticket) return;
    setMfaToken(ticket);
    setMfaCode('');
    setUseRecoveryCode(false);
    setStep('mfa');
  }, [corporateMfaToken]);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  const toLoginError = useCallback(
    (err: unknown, fallback: string): LoginError => {
      const platform = describeError?.(err);
      if (platform !== undefined) {
        const described = describeServerFailure(tCommon, err, fallback);
        return {
          text: platform,
          detail: described.detail === platform ? undefined : described.detail,
        };
      }
      const described = describeServerFailure(
        tCommon,
        err,
        isTransportFailure(err) ? t(($) => $.errors.unreachable) : fallback,
      );
      return described;
    },
    [describeError, t, tCommon],
  );

  const failWith = useCallback(
    (err: unknown, fallback: string) => {
      setFailure({ kind: 'request', err, fallback });
      onError?.(err);
    },
    [onError],
  );

  const error: LoginError | null =
    failure === null
      ? null
      : failure.kind === 'message'
        ? { text: failure.text }
        : toLoginError(failure.err, failure.fallback);

  const handleSendCode = useCallback(
    async (e?: React.FormEvent) => {
      e?.preventDefault();
      if (!email) {
        setFailure({ kind: 'message', text: t(($) => $.common.email_required) });
        return;
      }
      setLoading(true);
      setFailure(null);
      try {
        await useAuthStore.getState().sendCode(email);
        setStep('code');
        setCode('');
        setCooldown(60);
      } catch (err) {
        failWith(err, `${t(($) => $.errors.send_failed)} ${t(($) => $.errors.server_unreachable)}`);
      } finally {
        setLoading(false);
      }
    },
    [email, failWith, t],
  );

  const finishSession = useCallback(
    async (token: string) => {
      if (cliCallback) {
        localStorage.setItem('goosar_token', token);
        api.setToken(token);
        onTokenObtained?.();
        redirectToCliCallback(cliCallback.url, token, cliCallback.state);
        return;
      }
      await useAuthStore.getState().loginWithToken(token);
      const wsList = await api.listWorkspaces();
      qc.setQueryData(workspaceKeys.list(), wsList);
      onTokenObtained?.();
      onSuccess();
    },
    [cliCallback, onTokenObtained, onSuccess, qc],
  );

  const handleFirstFactorResult = useCallback(
    async (result: { token: string; mfa_required: boolean; mfa_token: string }) => {
      if (result.mfa_required) {
        setMfaToken(result.mfa_token);
        setMfaCode('');
        setUseRecoveryCode(false);
        setStep('mfa');
        setLoading(false);
        return;
      }
      if (!result.token) {
        throw new Error('Login failed: server returned a malformed response.');
      }
      await finishSession(result.token);
    },
    [finishSession],
  );

  const handleVerify = useCallback(
    async (value: string) => {
      if (value.length !== 6) return;
      setLoading(true);
      setFailure(null);
      try {
        await handleFirstFactorResult(await api.verifyCode(email, value));
      } catch (err) {
        failWith(
          err,
          t(($) => $.errors.code_invalid),
        );
        setCode('');
        setLoading(false);
      }
    },
    [email, handleFirstFactorResult, t, failWith],
  );

  const handleVerifyMFA = useCallback(
    async (value: string) => {
      setLoading(true);
      setFailure(null);
      try {
        const result = await api.verifyMFA({
          mfa_token: mfaToken,
          ...(useRecoveryCode ? { recovery_code: value } : { code: value }),
        });
        if (!result.token) {
          throw new Error('Login failed: server returned a malformed response.');
        }
        await finishSession(result.token);
      } catch (err) {
        failWith(
          err,
          t(($) => $.mfa.failed),
        );
        setMfaCode('');
        setLoading(false);
      }
    },
    [mfaToken, useRecoveryCode, finishSession, failWith, t],
  );

  const handleResend = async () => {
    if (cooldown > 0) return;
    setFailure(null);
    try {
      await useAuthStore.getState().sendCode(email);
      setCooldown(60);
    } catch (err) {
      failWith(
        err,
        t(($) => $.errors.resend_failed),
      );
    }
  };

  const handleCorporateLogin = useCallback(() => {
    setFailure(null);
    onCorporateLogin?.(api.oidcStartURL(corporateClient));
  }, [onCorporateLogin, corporateClient]);

  const handleLdapLogin = useCallback(
    async (username: string, password: string) => {
      setFailure(null);
      try {
        await handleFirstFactorResult(await api.ldapLogin(username, password));
      } catch (err) {
        failWith(
          err,
          t(($) => $.corporate.failed),
        );
        throw err;
      }
    },
    [handleFirstFactorResult, failWith, t],
  );

  const handleCliAuthorize = async () => {
    if (!cliCallback) return;
    setLoading(true);

    try {
      let token: string;

      if (authSourceRef.current === 'localStorage') {
        const stored = localStorage.getItem('goosar_token');
        if (!stored) throw new Error('token missing');
        token = stored;
      } else {
        const res = await api.issueCliToken();
        token = res.token;
      }

      onTokenObtained?.();
      redirectToCliCallback(cliCallback.url, token, cliCallback.state);
    } catch (err) {
      failWith(
        err,
        t(($) => $.errors.cli_auth_failed),
      );
      setExistingUser(null);
      setStep('email');
      setLoading(false);
    }
  };

  if (step === 'cli_confirm' && existingUser) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            {logo && <div className="mx-auto mb-4">{logo}</div>}
            <CardTitle className="text-2xl">{t(($) => $.cli.title)}</CardTitle>
            <CardDescription>
              {t(($) => $.cli.description, { email: existingUser.email })}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Button onClick={handleCliAuthorize} disabled={loading} className="w-full" size="lg">
              {loading ? t(($) => $.cli.authorizing) : t(($) => $.cli.authorize)}
            </Button>
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                setExistingUser(null);
                setStep('email');
              }}
            >
              {t(($) => $.cli.different_account)}
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (step === 'code') {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            {logo && <div className="mx-auto mb-4">{logo}</div>}
            <CardTitle className="text-2xl">{t(($) => $.verify.title)}</CardTitle>
            <CardDescription>{t(($) => $.verify.description, { email })}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-4">
            <InputOTP
              autoFocus
              maxLength={6}
              value={code}
              onChange={(value) => {
                setCode(value);
                if (value.length === 6) handleVerify(value);
              }}
              disabled={loading}
            >
              <InputOTPGroup>
                <InputOTPSlot index={0} />
                <InputOTPSlot index={1} />
                <InputOTPSlot index={2} />
                <InputOTPSlot index={3} />
                <InputOTPSlot index={4} />
                <InputOTPSlot index={5} />
              </InputOTPGroup>
            </InputOTP>
            <LoginErrorNote error={error} />
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <button
                type="button"
                onClick={handleResend}
                disabled={cooldown > 0}
                className="text-primary underline-offset-4 hover:underline disabled:text-muted-foreground disabled:no-underline disabled:cursor-not-allowed"
              >
                {cooldown > 0
                  ? t(($) => $.verify.resend_cooldown, { seconds: cooldown })
                  : t(($) => $.verify.resend)}
              </button>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              type="button"
              variant="ghost"
              className="w-full"
              onClick={() => {
                setStep('email');
                setCode('');
                setFailure(null);
              }}
            >
              {t(($) => $.common.back)}
            </Button>
          </CardFooter>
        </Card>
      </div>
    );
  }

  if (step === 'mfa') {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            {logo && <div className="mx-auto mb-4">{logo}</div>}
            <CardTitle className="text-2xl">{t(($) => $.mfa.title)}</CardTitle>
            <CardDescription>
              {useRecoveryCode ? t(($) => $.mfa.recovery_description) : t(($) => $.mfa.description)}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-4">
            {useRecoveryCode ? (
              <form
                id="mfa-recovery-form"
                className="w-full space-y-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (mfaCode.trim()) handleVerifyMFA(mfaCode.trim());
                }}
              >
                <Label htmlFor="mfa-recovery">{t(($) => $.mfa.recovery_label)}</Label>
                <Input
                  id="mfa-recovery"
                  autoFocus
                  autoComplete="one-time-code"
                  value={mfaCode}
                  onChange={(e) => setMfaCode(e.target.value)}
                  disabled={loading}
                />
              </form>
            ) : (
              <InputOTP
                autoFocus
                maxLength={6}
                value={mfaCode}
                onChange={(value) => {
                  setMfaCode(value);
                  if (value.length === 6) handleVerifyMFA(value);
                }}
                disabled={loading}
              >
                <InputOTPGroup>
                  <InputOTPSlot index={0} />
                  <InputOTPSlot index={1} />
                  <InputOTPSlot index={2} />
                  <InputOTPSlot index={3} />
                  <InputOTPSlot index={4} />
                  <InputOTPSlot index={5} />
                </InputOTPGroup>
              </InputOTP>
            )}
            <LoginErrorNote error={error} />
            <button
              type="button"
              className="text-sm text-primary underline-offset-4 hover:underline"
              onClick={() => {
                setUseRecoveryCode((prev) => !prev);
                setMfaCode('');
                setFailure(null);
              }}
            >
              {useRecoveryCode ? t(($) => $.mfa.use_app) : t(($) => $.mfa.use_recovery)}
            </button>
          </CardContent>
          <CardFooter className="flex flex-col gap-2">
            {useRecoveryCode && (
              <Button
                type="submit"
                form="mfa-recovery-form"
                className="w-full"
                size="lg"
                disabled={loading || !mfaCode.trim()}
              >
                {loading ? t(($) => $.mfa.verifying) : t(($) => $.mfa.submit)}
              </Button>
            )}
            <Button
              type="button"
              variant="ghost"
              className="w-full"
              onClick={() => {
                setMfaToken('');
                setMfaCode('');
                setUseRecoveryCode(false);
                setStep('email');
                setCode('');
                setFailure(null);
              }}
            >
              {t(($) => $.common.back)}
            </Button>
          </CardFooter>
        </Card>
      </div>
    );
  }

  const emailOffered = hasMethod(authMethods.methods, 'email');
  const ldapOffered = hasMethod(authMethods.methods, 'ldap');
  const oidcOffered = hasMethod(authMethods.methods, 'oidc') && onCorporateLogin !== undefined;
  const showLdap = ldapOffered && (door === 'ldap' || !emailOffered);

  return (
    <div className="flex min-h-svh items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          {logo && <div className="mx-auto mb-4">{logo}</div>}
          <CardTitle className="text-2xl">
            {showLdap ? t(($) => $.corporate.ldap_title) : t(($) => $.signin.title)}
          </CardTitle>
          {!showLdap && <CardDescription>{t(($) => $.signin.description)}</CardDescription>}
        </CardHeader>
        <CardContent>
          {showLdap ? (
            <LdapSignInForm
              onSubmit={handleLdapLogin}
              onAttempt={() => setFailure(null)}
              errorSlot={<LoginErrorNote error={error} />}
              disabled={loading}
            />
          ) : (
            <form id="login-form" onSubmit={handleSendCode} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="login-email">{t(($) => $.common.email)}</Label>
                <Input
                  id="login-email"
                  type="email"
                  placeholder={t(($) => $.common.email_placeholder)}
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoFocus
                  required
                />
              </div>
              <LoginErrorNote error={error} />
              <MailTransportNotice />
            </form>
          )}
          {corporateErrorCode && (
            <p className="w-full pt-3 text-sm break-words text-destructive" role="alert">
              {serverErrorMessage(tCommon, corporateErrorCode) ?? t(($) => $.corporate.failed)}
            </p>
          )}
        </CardContent>
        <CardFooter className="flex flex-col gap-3">
          {!showLdap && emailOffered && (
            <Button
              type="submit"
              form="login-form"
              className="w-full"
              size="lg"
              disabled={!email || loading}
            >
              {loading ? t(($) => $.signin.sending) : t(($) => $.signin.continue)}
            </Button>
          )}
          {oidcOffered && (
            <CorporateSignInButton
              displayName={authMethods.oidc_display_name}
              onClick={handleCorporateLogin}
              disabled={loading}
            />
          )}
          {/* The switch between the two forms, shown only when there is
              actually a choice to make. */}
          {ldapOffered && emailOffered && (
            <Button
              type="button"
              variant="ghost"
              className="w-full"
              onClick={() => {
                setFailure(null);
                setDoor(showLdap ? 'email' : 'ldap');
              }}
            >
              {showLdap
                ? t(($) => $.corporate.or_email)
                : authMethods.ldap_display_name?.trim() || t(($) => $.corporate.or_corporate)}
            </Button>
          )}
          {extra && <div className="w-full pt-1 text-center">{extra}</div>}
        </CardFooter>
      </Card>
    </div>
  );
}
