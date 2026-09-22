'use client';

import { useCallback, useEffect, useState } from 'react';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { api, type AuthMethod, EMPTY_AUTH_METHODS } from '@goosar/core/api';
import { useT } from '../i18n';

export function useAuthMethods() {
  const [methods, setMethods] = useState(EMPTY_AUTH_METHODS);

  useEffect(() => {
    let cancelled = false;
    void Promise.resolve()
      .then(() => api.getAuthMethods())
      .then((result) => {
        if (!cancelled) setMethods(result);
      })
      .catch(() => {
        // A server too old to know this endpoint, or one that is simply not
        // answering yet. Either way the e-mail form is the honest fallback:
        // the person came here to sign in, and hiding the only door we are
        // sure about would help nobody.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return methods;
}

export function hasMethod(methods: readonly AuthMethod[], want: AuthMethod) {
  return methods.includes(want);
}

interface CorporateButtonProps {
  displayName?: string;
  onClick: () => void;
  disabled?: boolean;
}

export function CorporateSignInButton({ displayName, onClick, disabled }: CorporateButtonProps) {
  const { t } = useT('auth');
  return (
    <Button
      type="button"
      variant="outline"
      className="w-full"
      size="lg"
      onClick={onClick}
      disabled={disabled}
    >
      {displayName?.trim() || t(($) => $.corporate.oidc_button)}
    </Button>
  );
}

interface LdapFormProps {
  onSubmit: (username: string, password: string) => Promise<void>;
  errorSlot?: React.ReactNode;
  onAttempt?: () => void;
  disabled?: boolean;
}

export function LdapSignInForm({ onSubmit, errorSlot, onAttempt, disabled }: LdapFormProps) {
  const { t } = useT('auth');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setLocalError(null);
      onAttempt?.();
      if (!username.trim() || !password) {
        setLocalError(t(($) => $.corporate.credentials_required));
        return;
      }
      setSubmitting(true);
      try {
        await onSubmit(username, password);
      } catch {
        setPassword('');
      } finally {
        setSubmitting(false);
      }
    },
    [username, password, onSubmit, onAttempt, t],
  );

  const busy = submitting || disabled;

  return (
    <form onSubmit={handleSubmit} className="w-full space-y-4">
      <div className="space-y-2">
        <Label htmlFor="ldap-username">{t(($) => $.corporate.username)}</Label>
        <Input
          id="ldap-username"
          name="username"
          autoComplete="username"
          placeholder={t(($) => $.corporate.username_placeholder)}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          disabled={busy}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="ldap-password">{t(($) => $.corporate.password)}</Label>
        <Input
          id="ldap-password"
          name="password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={busy}
        />
      </div>
      {localError && (
        <p className="text-sm break-words text-destructive" role="alert">
          {localError}
        </p>
      )}
      {errorSlot}
      <Button type="submit" className="w-full" size="lg" disabled={busy}>
        {submitting ? t(($) => $.corporate.submitting) : t(($) => $.corporate.submit)}
      </Button>
    </form>
  );
}
