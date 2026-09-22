'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { MFARequiredError, useAuthStore } from '@goosar/core/auth';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@goosar/ui/components/ui/card';
import { Button } from '@goosar/ui/components/ui/button';
import { Loader2 } from 'lucide-react';
import { setLoggedInCookie } from '@/features/auth/auth-cookie';
import { useT } from '@goosar/views/i18n';

function tokenFromHash(hash: string): string {
  if (!hash.startsWith('#')) return '';
  return new URLSearchParams(hash.slice(1)).get('lt') ?? '';
}

function deepLinkFor(token: string): string {
  return `goosar://auth/callback?link_token=${encodeURIComponent(token)}`;
}

type Phase = 'opening' | 'verifying' | 'failed' | 'missing';

export default function AuthLinkPage() {
  const router = useRouter();
  const { t } = useT('auth');

  const [token, setToken] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>('opening');
  const attemptedDeepLink = useRef(false);

  useEffect(() => {
    const parsed = tokenFromHash(window.location.hash);
    if (window.location.hash) {
      window.history.replaceState(null, '', window.location.pathname);
    }
    setToken(parsed);
    if (!parsed) {
      setPhase('missing');
      return;
    }
    if (!attemptedDeepLink.current) {
      attemptedDeepLink.current = true;
      window.location.href = deepLinkFor(parsed);
    }
  }, []);

  const handleContinueInBrowser = useCallback(async () => {
    if (!token) return;
    setPhase('verifying');
    try {
      await useAuthStore.getState().loginWithLinkToken(token);
      setLoggedInCookie();
      router.replace('/login');
    } catch (err) {
      if (err instanceof MFARequiredError && err.mfaToken) {
        router.replace(`/login#mfa_token=${encodeURIComponent(err.mfaToken)}`);
        return;
      }
      setPhase('failed');
    }
  }, [token, router]);

  if (token === null) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (phase === 'missing' || phase === 'failed') {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">{t(($) => $.web.login_link.failed_title)}</CardTitle>
            <CardDescription>
              {phase === 'missing'
                ? t(($) => $.web.login_link.missing_token)
                : t(($) => $.web.login_link.failed_description)}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex justify-center">
            <Link
              href="/login"
              className="text-sm font-medium text-foreground underline decoration-foreground/30 underline-offset-4 hover:decoration-foreground/70"
            >
              {t(($) => $.web.login_link.go_to_login)}
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">{t(($) => $.web.login_link.opening_title)}</CardTitle>
          <CardDescription>{t(($) => $.web.login_link.opening_description)}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col items-center gap-3">
          {phase === 'verifying' ? (
            <>
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
              <span className="text-sm text-muted-foreground">
                {t(($) => $.web.login_link.signing_in)}
              </span>
            </>
          ) : (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  window.location.href = deepLinkFor(token);
                }}
              >
                {t(($) => $.web.login_link.open_app_button)}
              </Button>
              <Button onClick={handleContinueInBrowser}>
                {t(($) => $.web.login_link.continue_browser)}
              </Button>
              <Link
                href="/login"
                className="text-xs text-muted-foreground underline decoration-foreground/30 underline-offset-4 hover:decoration-foreground/70"
              >
                {t(($) => $.web.login_link.go_to_login)}
              </Link>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
