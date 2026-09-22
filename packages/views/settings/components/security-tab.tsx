'use client';

import { useCallback, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import QRCode from 'react-qr-code';
import { api } from '@goosar/core/api';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@goosar/ui/components/ui/card';
import { serverErrorMessage } from '../../common/server-error';
import { useT } from '../../i18n';

const mfaKeys = {
  status: ['auth', 'mfa', 'status'] as const,
  sessions: ['auth', 'sessions'] as const,
};

function RecoveryCodes({ codes }: { codes: string[] }) {
  const { t } = useT('settings');
  if (codes.length === 0) return null;
  return (
    <div className="space-y-2 rounded-md border border-surface-border p-3">
      <p className="text-sm font-medium">{t(($) => $.security.recovery_title)}</p>
      <p className="text-xs text-muted-foreground">{t(($) => $.security.recovery_warning)}</p>
      <ul className="grid grid-cols-2 gap-1 font-mono text-sm">
        {codes.map((code) => (
          <li key={code} className="break-all">
            {code}
          </li>
        ))}
      </ul>
    </div>
  );
}

function EnrollmentFlow({ onDone }: { onDone: () => void }) {
  const { t } = useT('settings');
  const { t: tCommon } = useT('common');
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);

  const enroll = useQuery({
    queryKey: ['auth', 'mfa', 'enroll'],
    queryFn: () => api.enrollTOTP(),
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });

  const confirm = useMutation({
    mutationFn: (value: string) => api.confirmTOTP(value),
    onSuccess: (result) => {
      setRecoveryCodes(result.recovery_codes);
      setError(null);
    },
    onError: (err: unknown) =>
      setError(
        describeMfaError(
          tCommon,
          err,
          t(($) => $.security.code_rejected),
        ),
      ),
  });

  if (recoveryCodes.length > 0) {
    return (
      <div className="space-y-3">
        <p className="text-sm">{t(($) => $.security.enabled_now)}</p>
        <RecoveryCodes codes={recoveryCodes} />
        <Button onClick={onDone}>{t(($) => $.security.recovery_saved)}</Button>
      </div>
    );
  }

  if (enroll.isPending) {
    return <p className="text-sm text-muted-foreground">{t(($) => $.security.loading)}</p>;
  }
  if (enroll.isError || !enroll.data?.otpauth_uri) {
    return (
      <p className="text-sm text-destructive" role="alert">
        {describeMfaError(
          tCommon,
          enroll.error,
          t(($) => $.security.enroll_failed),
        )}
      </p>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t(($) => $.security.scan_hint)}</p>
      {/* A fixed white ground, not a token: a QR code is scanned by a camera,
          and inverting it in dark mode makes it unreadable on many phones. */}
      <div className="w-fit rounded-md bg-white p-3">
        <QRCode value={enroll.data.otpauth_uri} size={160} />
      </div>
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">{t(($) => $.security.manual_hint)}</p>
        <p className="font-mono text-sm break-all">{enroll.data.secret}</p>
      </div>
      <form
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (code.trim()) confirm.mutate(code.trim());
        }}
      >
        <Label htmlFor="mfa-confirm-code">{t(($) => $.security.code_label)}</Label>
        <Input
          id="mfa-confirm-code"
          inputMode="numeric"
          autoComplete="one-time-code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
        />
        {error && (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        )}
        <div className="flex gap-2">
          <Button type="submit" disabled={!code.trim() || confirm.isPending}>
            {confirm.isPending ? t(($) => $.security.activating) : t(($) => $.security.activate)}
          </Button>
          <Button type="button" variant="ghost" onClick={onDone}>
            {t(($) => $.security.cancel)}
          </Button>
        </div>
      </form>
    </div>
  );
}

function describeMfaError(
  tCommon: ReturnType<typeof useT<'common'>>['t'],
  err: unknown,
  fallback: string,
): string {
  const code =
    typeof err === 'object' && err !== null && 'body' in err
      ? ((err as { body?: { code?: string } }).body?.code ?? undefined)
      : undefined;
  return serverErrorMessage(tCommon, code) ?? fallback;
}

function MFASection() {
  const { t } = useT('settings');
  const { t: tCommon } = useT('common');
  const qc = useQueryClient();
  const [enrolling, setEnrolling] = useState(false);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [freshCodes, setFreshCodes] = useState<string[]>([]);

  const status = useQuery({
    queryKey: mfaKeys.status,
    queryFn: () => api.getMFAStatus(),
  });

  const finishEnrollment = useCallback(() => {
    setEnrolling(false);
    qc.invalidateQueries({ queryKey: mfaKeys.status });
  }, [qc]);

  const disable = useMutation({
    mutationFn: (value: string) => api.disableTOTP(value),
    onSuccess: () => {
      setCode('');
      setError(null);
      setFreshCodes([]);
      qc.invalidateQueries({ queryKey: mfaKeys.status });
    },
    onError: (err: unknown) =>
      setError(
        describeMfaError(
          tCommon,
          err,
          t(($) => $.security.code_rejected),
        ),
      ),
  });

  const regenerate = useMutation({
    mutationFn: (value: string) => api.regenerateRecoveryCodes(value),
    onSuccess: (result) => {
      setCode('');
      setError(null);
      setFreshCodes(result.recovery_codes);
      qc.invalidateQueries({ queryKey: mfaKeys.status });
    },
    onError: (err: unknown) =>
      setError(
        describeMfaError(
          tCommon,
          err,
          t(($) => $.security.code_rejected),
        ),
      ),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(($) => $.security.mfa_title)}</CardTitle>
        <CardDescription>{t(($) => $.security.mfa_description)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {status.data?.available === false && (
          <p className="text-sm text-muted-foreground" role="status">
            {t(($) => $.security.unavailable)}
          </p>
        )}
        {status.data?.required === true && status.data?.enabled === false && (
          <p className="text-sm text-destructive" role="status">
            {t(($) => $.security.required_notice)}
          </p>
        )}

        {status.data?.enabled === true ? (
          <>
            <p className="text-sm">
              {t(($) => $.security.enabled_state, {
                count: status.data.recovery_codes_remaining,
              })}
            </p>
            <RecoveryCodes codes={freshCodes} />
            <form className="space-y-2" onSubmit={(e) => e.preventDefault()}>
              <Label htmlFor="mfa-current-code">{t(($) => $.security.current_code_label)}</Label>
              <Input
                id="mfa-current-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
              {error && (
                <p className="text-sm text-destructive" role="alert">
                  {error}
                </p>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!code.trim() || regenerate.isPending}
                  onClick={() => regenerate.mutate(code.trim())}
                >
                  {t(($) => $.security.regenerate)}
                </Button>
                <Button
                  type="button"
                  variant="destructive"
                  disabled={!code.trim() || disable.isPending}
                  onClick={() => disable.mutate(code.trim())}
                >
                  {t(($) => $.security.disable)}
                </Button>
              </div>
            </form>
          </>
        ) : enrolling ? (
          <EnrollmentFlow onDone={finishEnrollment} />
        ) : (
          <Button disabled={status.data?.available === false} onClick={() => setEnrolling(true)}>
            {t(($) => $.security.enable)}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

function SessionsSection() {
  const { t } = useT('settings');
  const qc = useQueryClient();

  const sessions = useQuery({
    queryKey: mfaKeys.sessions,
    queryFn: () => api.listSessions(),
  });

  const revokeOne = useMutation({
    mutationFn: (id: string) => api.revokeSession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: mfaKeys.sessions }),
  });

  const revokeAll = useMutation({
    mutationFn: () => api.revokeAllSessions(),
    onSuccess: () => window.location.reload(),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(($) => $.security.sessions_title)}</CardTitle>
        <CardDescription>{t(($) => $.security.sessions_description)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {sessions.isPending && (
          <p className="text-sm text-muted-foreground">{t(($) => $.security.loading)}</p>
        )}
        {sessions.data?.length === 0 && !sessions.isPending && (
          <p className="text-sm text-muted-foreground">{t(($) => $.security.sessions_empty)}</p>
        )}
        <ul className="space-y-2">
          {(sessions.data ?? []).map((session) => (
            <li
              key={session.id}
              className="flex items-center justify-between gap-3 rounded-md border border-surface-border p-2"
            >
              <div className="min-w-0">
                <p className="truncate text-sm">
                  {session.user_agent || t(($) => $.security.unknown_device)}
                  {session.current && ` — ${t(($) => $.security.this_device)}`}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.security.last_seen, { at: session.last_seen_at })}
                </p>
              </div>
              {!session.current && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={revokeOne.isPending}
                  onClick={() => revokeOne.mutate(session.id)}
                >
                  {t(($) => $.security.session_sign_out)}
                </Button>
              )}
            </li>
          ))}
        </ul>
        <Button
          variant="destructive"
          disabled={revokeAll.isPending}
          onClick={() => revokeAll.mutate()}
        >
          {t(($) => $.security.sign_out_everywhere)}
        </Button>
      </CardContent>
    </Card>
  );
}

export function SecurityTab() {
  return (
    <div className="space-y-6">
      <MFASection />
      <SessionsSection />
    </div>
  );
}
