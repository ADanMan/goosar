'use client';

import { ONBOARDING_STEP_ORDER, type OnboardingStep } from '@goosar/core/onboarding';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../../i18n';

export function StepHeader({ currentStep }: { currentStep: OnboardingStep }) {
  const { t } = useT('onboarding');
  const total = ONBOARDING_STEP_ORDER.length;
  const currentIndex = ONBOARDING_STEP_ORDER.indexOf(currentStep);
  const safeIndex = currentIndex === -1 ? 0 : currentIndex;

  return (
    <div
      role="progressbar"
      aria-valuemin={1}
      aria-valuemax={total}
      aria-valuenow={safeIndex + 1}
      aria-label={t(($) => $.step_header.step_of, { current: safeIndex + 1, total })}
      className="flex w-full items-center justify-between py-2"
    >
      <div className="flex items-center gap-2">
        {ONBOARDING_STEP_ORDER.map((stepId, i) => {
          const isDone = i < safeIndex;
          const isCurrent = i === safeIndex;
          return (
            <span
              key={stepId}
              aria-hidden
              className={cn(
                'h-2 w-2 rounded-full transition-colors',
                isDone && 'bg-primary',
                isCurrent &&
                  'bg-primary ring-2 ring-primary/30 ring-offset-2 ring-offset-background',
                !isDone && !isCurrent && 'bg-muted',
              )}
            />
          );
        })}
      </div>
      <span className="text-xs font-medium text-muted-foreground">
        {t(($) => $.step_header.step_of, { current: safeIndex + 1, total })}
      </span>
    </div>
  );
}
