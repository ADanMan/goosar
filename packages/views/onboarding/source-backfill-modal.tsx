'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Briefcase,
  CalendarDays,
  Globe,
  HelpCircle,
  MoreHorizontal,
  Newspaper,
  Users,
} from 'lucide-react';
import { toastApiError } from '../common/error-toast';
import { useAuthStore } from '@goosar/core/auth';
import {
  agentCompletedIssueCountOptions,
  needsSourceBackfill,
  saveQuestionnaire,
  SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES,
  type QuestionnaireAnswers,
  type Source,
} from '@goosar/core/onboarding';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { Button } from '@goosar/ui/components/ui/button';
import { Dialog, DialogContent } from '@goosar/ui/components/ui/dialog';
import {
  GitHubIcon,
  GoogleIcon,
  LinkedInIcon,
  OpenAIIcon,
  XIcon,
  YouTubeIcon,
} from './components/brand-icons';
import {
  IconOptionCard,
  IconOtherOptionCard,
  type QuestionOption,
} from './components/icon-option-card';
import { mergedQuestionnairePatch } from './source-backfill-merge';
import { useSourceBackfillDismissCount } from './source-backfill-dismiss';
import { useT } from '../i18n';

const EMPTY_BACKFILL: Pick<QuestionnaireAnswers, 'source' | 'source_other' | 'source_skipped'> = {
  source: [],
  source_other: null,
  source_skipped: false,
};

export function SourceBackfillModal() {
  const user = useAuthStore((s) => s.user);
  const userId = user?.id ?? null;
  const [dismissCount, bumpDismissCount] = useSourceBackfillDismissCount(userId);

  const userEligible = needsSourceBackfill(user, dismissCount);
  const wsId = useCurrentWorkspace()?.id ?? null;
  const { data: agentDoneCount } = useQuery({
    ...agentCompletedIssueCountOptions(wsId ?? ''),
    enabled: userEligible && wsId !== null,
  });
  const shouldPrompt =
    userEligible && (agentDoneCount ?? 0) >= SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES;

  const [open, setOpen] = useState(false);
  const openedForUserRef = useRef<string | null>(null);
  useEffect(() => {
    if (!user) {
      openedForUserRef.current = null;
      setOpen(false);
      return;
    }
    if (openedForUserRef.current === user.id) return;
    if (!shouldPrompt) return;
    const reducedMotion =
      typeof window !== 'undefined' &&
      window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
    if (reducedMotion) {
      openedForUserRef.current = user.id;
      setOpen(true);
      return;
    }
    const uid = user.id;
    const timer = window.setTimeout(() => {
      openedForUserRef.current = uid;
      setOpen(true);
    }, 700);
    return () => window.clearTimeout(timer);
  }, [user, shouldPrompt]);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next || !open) return;
        bumpDismissCount();
        setOpen(false);
      }}
    >
      <SourceBackfillDialogBody onComplete={() => setOpen(false)} />
    </Dialog>
  );
}

SourceBackfillModal.displayName = 'SourceBackfillModal';

function SourceBackfillDialogBody({ onComplete }: { onComplete: () => void }) {
  const { t } = useT('onboarding');

  const [answers, setAnswers] = useState(EMPTY_BACKFILL);
  const [busy, setBusy] = useState(false);

  const options = useMemo<QuestionOption[]>(
    () => [
      {
        slug: 'friends_colleagues',
        icon: <Users className="h-4 w-4" />,
        label: t(($) => $.questions.source.friends_colleagues),
      },
      {
        slug: 'search',
        icon: <GoogleIcon className="h-[18px] w-[18px]" />,
        label: t(($) => $.questions.source.search),
      },
      {
        slug: 'social_x',
        icon: <XIcon className="h-[15px] w-[15px]" />,
        label: t(($) => $.questions.source.social_x),
      },
      {
        slug: 'social_linkedin',
        icon: <LinkedInIcon className="h-[18px] w-[18px]" />,
        label: t(($) => $.questions.source.social_linkedin),
      },
      {
        slug: 'social_youtube',
        icon: <YouTubeIcon className="h-[18px] w-[18px]" />,
        label: t(($) => $.questions.source.social_youtube),
      },
      {
        slug: 'social_github',
        icon: <GitHubIcon className="h-[18px] w-[18px]" />,
        label: t(($) => $.questions.source.social_github),
      },
      {
        slug: 'social_other',
        icon: <Globe className="h-4 w-4" />,
        label: t(($) => $.questions.source.social_misc),
      },
      {
        slug: 'blog_newsletter',
        icon: <Newspaper className="h-4 w-4" />,
        label: t(($) => $.questions.source.blog_newsletter),
      },
      {
        slug: 'ai_assistant',
        icon: <OpenAIIcon className="h-[16px] w-[16px]" />,
        label: t(($) => $.questions.source.ai_assistant),
      },
      {
        slug: 'from_work',
        icon: <Briefcase className="h-4 w-4" />,
        label: t(($) => $.questions.source.from_work),
      },
      {
        slug: 'event_conference',
        icon: <CalendarDays className="h-4 w-4" />,
        label: t(($) => $.questions.source.event_conference),
      },
      {
        slug: 'dont_remember',
        icon: <HelpCircle className="h-4 w-4" />,
        label: t(($) => $.questions.source.dont_remember),
      },
      {
        slug: 'other',
        icon: <MoreHorizontal className="h-4 w-4" />,
        label: t(($) => $.questions.source.other),
        isOther: true,
      },
    ],
    [t],
  );

  const pickedSlug: string | null = answers.source[0] ?? null;
  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = pickedSlug === otherOption?.slug;
  const otherFilled = (answers.source_other ?? '').trim().length > 0;
  const canSubmit =
    !busy &&
    pickedSlug !== null &&
    (!otherSelected || otherFilled);

  const handleSelect = useCallback((option: QuestionOption) => {
    if (option.isOther) {
      const slug = option.slug as Source;
      setAnswers((a) => ({
        ...a,
        source: [slug],
        source_other: a.source[0] === 'other' ? a.source_other : null,
        source_skipped: false,
      }));
      return;
    }
    const slug = option.slug as Source;
    setAnswers((a) => ({
      ...a,
      source: [slug],
      source_other: null,
      source_skipped: false,
    }));
  }, []);

  const handleOtherChange = useCallback((value: string) => {
    setAnswers((a) => ({ ...a, source_other: value }));
  }, []);

  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setBusy(true);
    try {
      const stored = useAuthStore.getState().user?.onboarding_questionnaire ?? null;
      await saveQuestionnaire(
        mergedQuestionnairePatch(stored, {
          source: answers.source,
          source_other: answers.source_other,
          source_skipped: false,
        }),
      );
      onComplete();
    } catch (err) {
      setBusy(false);
      toastApiError(
        err,
        t(($) => $.errors.save_failed),
      );
    }
  }, [canSubmit, answers.source, answers.source_other, onComplete, t]);

  const skip = useCallback(async () => {
    if (busy) return;
    setBusy(true);
    try {
      const stored = useAuthStore.getState().user?.onboarding_questionnaire ?? null;
      await saveQuestionnaire(
        mergedQuestionnairePatch(stored, {
          source: [],
          source_other: null,
          source_skipped: true,
        }),
      );
      onComplete();
    } catch (err) {
      setBusy(false);
      toastApiError(
        err,
        t(($) => $.errors.save_failed),
      );
    }
  }, [busy, onComplete, t]);

  return (
    <DialogContent className="sm:max-w-2xl p-0 gap-0 overflow-hidden">
      <div className="px-6 pt-6 pb-2">
        <div className="text-[11px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.source_backfill.eyebrow)}
        </div>
        <h2 className="mt-1 text-balance font-serif text-2xl font-medium leading-tight tracking-tight text-foreground">
          {t(($) => $.questions.source.question)}
        </h2>
        <p className="mt-2 text-sm text-muted-foreground">{t(($) => $.source_backfill.lede)}</p>
      </div>

      <fieldset
        role="radiogroup"
        aria-label={t(($) => $.questions.source.question)}
        className="m-0 grid grid-cols-1 gap-2 p-0 px-6 pt-4 sm:grid-cols-2"
      >
        {options.map((option) =>
          option.isOther ? (
            <IconOtherOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={otherSelected}
              onSelect={() => handleSelect(option)}
              otherValue={answers.source_other ?? ''}
              onOtherChange={handleOtherChange}
              onConfirm={submit}
              placeholder={t(($) => $.questions.source.other_placeholder)}
              mode="radio"
            />
          ) : (
            <IconOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={pickedSlug === option.slug}
              onSelect={() => handleSelect(option)}
              mode="radio"
            />
          ),
        )}
      </fieldset>

      <div className="mt-4 flex flex-wrap items-center justify-end gap-x-4 gap-y-2 border-t bg-muted/40 px-6 py-3">
        <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
          {canSubmit ? t(($) => $.source_backfill.hint_ready) : t(($) => $.step_question.hint_pick)}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="secondary" disabled={busy} onClick={skip}>
            {t(($) => $.common.skip)}
          </Button>
          <Button disabled={!canSubmit} onClick={submit}>
            {t(($) => $.source_backfill.submit)}
          </Button>
        </div>
      </div>
    </DialogContent>
  );
}
