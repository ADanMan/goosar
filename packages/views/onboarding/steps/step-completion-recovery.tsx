'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Check, Loader2, TriangleAlert } from 'lucide-react';
import { describeApiFailure, type ApiFailure } from '@goosar/core/api';
import { retryOnboardingCompletionDelivery } from '@goosar/core/onboarding';
import { paths } from '@goosar/core/paths';
import { myInvitationListOptions } from '@goosar/core/workspace/queries';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@goosar/ui/components/ui/collapsible';
import { DragStrip } from '@goosar/views/platform';
import { useLogout } from '../../auth';
import { useNavigation } from '../../navigation';
import { useT } from '../../i18n';

type DeliveryFailure =
  | 'unreachable'
  /** The server answered and would not take it. */
  | 'server_error'
  /** 401/403 — the token died while the app was closed. */
  | 'session_expired'
  /** The request went through; the server still reports no `onboarded_at`. */
  | 'unconfirmed'
  /** The request went through; reading the result back failed. */
  | 'delivered_unverified'
  /** A throw that is neither transport nor HTTP — a fault on our side. */
  | 'unknown';

type Phase =
  | { kind: 'sending' }
  | { kind: 'confirmed' }
  | { kind: 'failed'; outcome: DeliveryFailure; detail: string | null };

function classifyFailure(failure: ApiFailure): DeliveryFailure {
  if (failure.status === 401 || failure.status === 403) {
    return 'session_expired';
  }
  if (failure.kind === 'unreachable') return 'unreachable';
  if (failure.kind === 'server_error') return 'server_error';
  return 'unknown';
}

export function StepCompletionRecovery({
  onRecovered,
  onStartOver,
}: {
  onRecovered: () => void | Promise<void>;
  onStartOver: () => void;
}) {
  const { t } = useT('onboarding');
  const { push } = useNavigation();
  const logout = useLogout();
  const [phase, setPhase] = useState<Phase>({ kind: 'sending' });
  const attemptedRef = useRef(false);
  const mountedRef = useRef(true);

  const { data: invitations } = useQuery(myInvitationListOptions());
  const hasInvitation = (invitations?.length ?? 0) > 0;

  const onRecoveredRef = useRef(onRecovered);
  onRecoveredRef.current = onRecovered;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const deliver = useCallback(async () => {
    setPhase({ kind: 'sending' });
    try {
      const result = await retryOnboardingCompletionDelivery();
      if (!mountedRef.current) return;
      if (result === 'confirmed') {
        setPhase({ kind: 'confirmed' });
        void onRecoveredRef.current();
        return;
      }
      setPhase({
        kind: 'failed',
        outcome: result === 'delivered_unverified' ? 'delivered_unverified' : 'unconfirmed',
        detail: null,
      });
    } catch (err) {
      if (!mountedRef.current) return;
      const failure = describeApiFailure(err);
      setPhase({
        kind: 'failed',
        outcome: classifyFailure(failure),
        detail: failure.message ?? failure.detail,
      });
    }
  }, []);

  useEffect(() => {
    if (attemptedRef.current) return;
    attemptedRef.current = true;
    void deliver();
  }, [deliver]);

  const statusText = (() => {
    if (phase.kind === 'sending') {
      return t(($) => $.step_completion_recovery.status_sending);
    }
    if (phase.kind === 'confirmed') {
      return t(($) => $.step_completion_recovery.status_confirmed);
    }
    switch (phase.outcome) {
      case 'unreachable':
        return t(($) => $.step_completion_recovery.status_unreachable);
      case 'server_error':
        return t(($) => $.step_completion_recovery.status_server_error);
      case 'session_expired':
        return t(($) => $.step_completion_recovery.status_session_expired);
      case 'unconfirmed':
        return t(($) => $.step_completion_recovery.status_unconfirmed);
      case 'delivered_unverified':
        return t(($) => $.step_completion_recovery.status_delivered_unverified);
      default:
        return t(($) => $.step_completion_recovery.status_unknown);
    }
  })();

  const headline = (() => {
    if (phase.kind === 'sending') {
      return t(($) => $.step_completion_recovery.headline_sending);
    }
    if (phase.kind === 'confirmed') {
      return t(($) => $.step_completion_recovery.headline_confirmed);
    }
    return t(($) => $.step_completion_recovery.headline);
  })();

  const lede = (() => {
    if (phase.kind === 'sending') {
      return t(($) => $.step_completion_recovery.lede_sending);
    }
    if (phase.kind === 'confirmed') {
      return t(($) => $.step_completion_recovery.lede_confirmed);
    }
    return t(($) => $.step_completion_recovery.lede);
  })();

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      <DragStrip />
      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[620px] px-6 py-16 sm:px-10 md:px-14 lg:px-0 lg:py-24">
          <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
            {headline}
          </h1>
          <p className="mt-4 text-[15.5px] leading-[1.55] text-muted-foreground">{lede}</p>

          <p
            aria-live="polite"
            className="mt-8 flex items-start gap-2 text-[13.5px] leading-[1.55]"
          >
            <StatusIcon phase={phase.kind} />
            <span
              className={phase.kind === 'failed' ? 'text-destructive' : 'text-muted-foreground'}
            >
              {statusText}
            </span>
          </p>

          {phase.kind === 'failed' && phase.detail && <FailureDetail detail={phase.detail} />}

          {phase.kind === 'failed' && hasInvitation && (
            <div className="mt-6 rounded-md border border-border bg-muted/40 p-4">
              <p className="text-[13.5px] leading-[1.55] text-foreground">
                {t(($) => $.step_completion_recovery.invitation_notice)}
              </p>
              <Button
                variant="outline"
                size="sm"
                className="mt-3"
                onClick={() => push(paths.invitations())}
              >
                {t(($) => $.step_completion_recovery.open_invitations)}
              </Button>
            </div>
          )}

          {phase.kind !== 'confirmed' && (
            <>
              <div className="mt-8 flex flex-wrap items-center gap-3">
                {phase.kind === 'failed' && phase.outcome === 'session_expired' ? (
                  <Button size="lg" onClick={logout}>
                    {t(($) => $.step_completion_recovery.sign_out)}
                  </Button>
                ) : (
                  <Button
                    size="lg"
                    onClick={() => void deliver()}
                    disabled={phase.kind !== 'failed'}
                  >
                    {t(($) => $.step_completion_recovery.retry)}
                  </Button>
                )}
                <Button size="lg" variant="ghost" onClick={onStartOver}>
                  {t(($) => $.step_completion_recovery.start_over)}
                </Button>
              </div>
              <p className="mt-3 text-xs leading-[1.55] text-muted-foreground">
                {t(($) => $.step_completion_recovery.start_over_hint)}
              </p>
            </>
          )}
        </div>
      </main>
    </div>
  );
}

function StatusIcon({ phase }: { phase: Phase['kind'] }) {
  if (phase === 'failed') {
    return <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" aria-hidden />;
  }
  if (phase === 'confirmed') {
    return <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-foreground" aria-hidden />;
  }
  return (
    <Loader2
      className="mt-0.5 h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground"
      aria-hidden
    />
  );
}

function FailureDetail({ detail }: { detail: string }) {
  const { t } = useT('onboarding');
  const [open, setOpen] = useState(false);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="mt-2 pl-[22px]">
      <CollapsibleTrigger className="text-xs text-muted-foreground underline-offset-4 hover:underline">
        {t(($) => $.step_completion_recovery.details)}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="mt-1 max-h-32 overflow-auto rounded bg-muted/40 p-2 text-xs whitespace-pre-wrap break-all text-muted-foreground">
          {detail}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}
