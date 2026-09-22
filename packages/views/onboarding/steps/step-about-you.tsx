'use client';

import { useRef } from 'react';
import {
  ArrowLeft,
  ArrowRight,
  Brain,
  Briefcase,
  Code2,
  Compass,
  FileEdit,
  GraduationCap,
  ListChecks,
  Megaphone,
  MoreHorizontal,
  Palette,
  PenLine,
  Rocket,
  Search,
  Settings2,
  User,
} from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import type { QuestionnaireAnswers, Role, UseCase } from '@goosar/core/onboarding';
import { DragStrip } from '@goosar/views/platform';
import { StepHeader } from '../components/step-header';
import {
  IconOptionCard,
  IconOtherOptionCard,
  type QuestionOption,
} from '../components/icon-option-card';
import { useT } from '../../i18n';

export function StepAboutYou({
  answers,
  onChange,
  onAdvance,
  onSkip,
  onBack,
}: {
  answers: QuestionnaireAnswers;
  onChange: (patch: Partial<QuestionnaireAnswers>) => void;
  onAdvance: () => void;
  onSkip: () => void;
  onBack?: () => void;
}) {
  const { t } = useT('onboarding');
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const roleOptions: QuestionOption[] = [
    {
      slug: 'engineer',
      icon: <Code2 className="h-4 w-4" />,
      label: t(($) => $.questions.role.engineer),
    },
    {
      slug: 'product',
      icon: <Briefcase className="h-4 w-4" />,
      label: t(($) => $.questions.role.product),
    },
    {
      slug: 'designer',
      icon: <Palette className="h-4 w-4" />,
      label: t(($) => $.questions.role.designer),
    },
    {
      slug: 'founder',
      icon: <Rocket className="h-4 w-4" />,
      label: t(($) => $.questions.role.founder),
    },
    {
      slug: 'marketing',
      icon: <Megaphone className="h-4 w-4" />,
      label: t(($) => $.questions.role.marketing),
    },
    {
      slug: 'writer',
      icon: <PenLine className="h-4 w-4" />,
      label: t(($) => $.questions.role.writer),
    },
    {
      slug: 'research',
      icon: <Search className="h-4 w-4" />,
      label: t(($) => $.questions.role.research),
    },
    { slug: 'ops', icon: <Settings2 className="h-4 w-4" />, label: t(($) => $.questions.role.ops) },
    {
      slug: 'student',
      icon: <GraduationCap className="h-4 w-4" />,
      label: t(($) => $.questions.role.student),
    },
    {
      slug: 'other',
      icon: <MoreHorizontal className="h-4 w-4" />,
      label: t(($) => $.questions.role.other),
      isOther: true,
    },
  ];

  const useCaseOptions: QuestionOption[] = [
    {
      slug: 'ship_code',
      icon: <Code2 className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.ship_code),
    },
    {
      slug: 'manage_team',
      icon: <ListChecks className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.manage_team),
    },
    {
      slug: 'personal_tasks',
      icon: <User className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.personal_tasks),
    },
    {
      slug: 'plan_research',
      icon: <Brain className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.plan_research),
    },
    {
      slug: 'write_publish',
      icon: <FileEdit className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.write_publish),
    },
    {
      slug: 'automate_ops',
      icon: <Settings2 className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.automate_ops),
    },
    {
      slug: 'evaluate',
      icon: <Compass className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.evaluate),
    },
    {
      slug: 'other',
      icon: <MoreHorizontal className="h-4 w-4" />,
      label: t(($) => $.questions.use_case.other),
      isOther: true,
    },
  ];

  const roleSelected: readonly string[] = answers.role ? [answers.role] : [];
  const roleOtherFilled = (answers.role_other ?? '').trim().length > 0;
  const roleAnswered = answers.role !== null && (answers.role !== 'other' || roleOtherFilled);

  const useCaseSlugs = answers.use_case ?? [];
  const useCaseOtherFilled = (answers.use_case_other ?? '').trim().length > 0;
  const useCaseHasNonOther = useCaseSlugs.some((s) => s !== 'other');
  const useCaseAnswered = useCaseSlugs.length > 0 && (useCaseHasNonOther || useCaseOtherFilled);

  const canContinue = roleAnswered || useCaseAnswered;

  const pickRole = (slug: string) => {
    if (slug === 'other') {
      onChange({ role: 'other', role_skipped: false });
      return;
    }
    onChange({ role: slug as Role, role_other: null, role_skipped: false });
  };

  const toggleUseCase = (slug: string) => {
    const current = answers.use_case ?? [];
    if (slug === 'other') {
      if (current.includes('other')) {
        onChange({
          use_case: current.filter((s) => s !== 'other'),
          use_case_other: null,
        });
      } else {
        onChange({ use_case: [...current, 'other'], use_case_skipped: false });
      }
      return;
    }
    const typed = slug as UseCase;
    const next = current.includes(typed) ? current.filter((s) => s !== typed) : [...current, typed];
    onChange({ use_case: next, use_case_skipped: false });
  };

  const confirmAdvance = () => {
    if (!canContinue) return;
    const patch: Partial<QuestionnaireAnswers> = {};
    if (!roleAnswered) {
      patch.role = null;
      patch.role_other = null;
      patch.role_skipped = true;
    }
    if (!useCaseAnswered) {
      patch.use_case = [];
      patch.use_case_other = null;
      patch.use_case_skipped = true;
    }
    if (Object.keys(patch).length > 0) onChange(patch);
    onAdvance();
  };

  const handleSkip = () => {
    onChange({
      role: null,
      role_other: null,
      role_skipped: true,
      use_case: [],
      use_case_other: null,
      use_case_skipped: true,
    });
    onSkip();
  };

  const footerHint = canContinue
    ? t(($) => $.step_question.hint_continue)
    : t(($) => $.step_question.hint_pick);

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      <DragStrip />
      <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            {t(($) => $.common.back)}
          </button>
        ) : (
          <span aria-hidden className="w-0" />
        )}
        <div className="flex-1">
          <StepHeader currentStep="about_you" />
        </div>
      </header>

      <main ref={mainRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[920px] px-6 py-10 sm:px-10 md:px-14 lg:py-14">
          <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            {t(($) => $.questions.eyebrow_about_you)}
          </div>
          <h1 className="text-balance font-serif text-[34px] font-medium leading-[1.15] tracking-tight text-foreground">
            {t(($) => $.questions.about_you.question)}
          </h1>

          <QuestionGroup
            number={1}
            question={t(($) => $.questions.role.question)}
            options={roleOptions}
            selectedSlugs={roleSelected}
            otherValue={answers.role_other ?? ''}
            onOtherChange={(v) => onChange({ role_other: v })}
            otherPlaceholder={t(($) => $.questions.role.other_placeholder)}
            onAnswer={pickRole}
            onConfirm={confirmAdvance}
          />

          <QuestionGroup
            number={2}
            question={t(($) => $.questions.use_case.question)}
            options={useCaseOptions}
            selectedSlugs={useCaseSlugs}
            otherValue={answers.use_case_other ?? ''}
            onOtherChange={(v) => onChange({ use_case_other: v })}
            otherPlaceholder={t(($) => $.questions.use_case.other_placeholder)}
            onAnswer={toggleUseCase}
            onConfirm={confirmAdvance}
            multiSelect
          />

          <div className="mt-10 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
              {footerHint}
            </span>
            <div className="flex items-center gap-2">
              <Button size="lg" variant="secondary" onClick={handleSkip}>
                {t(($) => $.common.skip)}
              </Button>
              <Button size="lg" disabled={!canContinue} onClick={confirmAdvance}>
                {t(($) => $.common.continue)}
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

StepAboutYou.displayName = 'StepAboutYou';

function QuestionGroup({
  number,
  question,
  options,
  selectedSlugs,
  otherValue,
  onOtherChange,
  otherPlaceholder,
  onAnswer,
  onConfirm,
  multiSelect = false,
}: {
  number: number;
  question: string;
  options: readonly QuestionOption[];
  selectedSlugs: readonly string[];
  otherValue: string;
  onOtherChange: (value: string) => void;
  otherPlaceholder: string;
  onAnswer: (slug: string) => void;
  onConfirm: () => void;
  multiSelect?: boolean;
}) {
  const otherOption = options.find((o) => o.isOther) ?? null;
  const otherSelected = otherOption ? selectedSlugs.includes(otherOption.slug) : false;

  return (
    <section className="mt-10">
      <div className="flex items-baseline gap-3">
        <span aria-hidden className="font-mono text-xs text-muted-foreground">
          {String(number).padStart(2, '0')}
        </span>
        <h2 className="text-[17px] font-medium leading-snug text-foreground">{question}</h2>
      </div>
      <fieldset
        role={multiSelect ? 'group' : 'radiogroup'}
        aria-label={question}
        className="m-0 mt-4 grid grid-cols-1 gap-3 p-0 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
      >
        {options.map((option) =>
          option.isOther ? (
            <IconOtherOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={otherSelected}
              onSelect={() => onAnswer(option.slug)}
              otherValue={otherValue}
              onOtherChange={onOtherChange}
              onConfirm={onConfirm}
              placeholder={otherPlaceholder}
              mode={multiSelect ? 'checkbox' : 'radio'}
            />
          ) : (
            <IconOptionCard
              key={option.slug}
              icon={option.icon}
              label={option.label}
              selected={selectedSlugs.includes(option.slug)}
              onSelect={() => onAnswer(option.slug)}
              mode={multiSelect ? 'checkbox' : 'radio'}
            />
          ),
        )}
      </fieldset>
    </section>
  );
}
