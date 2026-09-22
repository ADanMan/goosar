'use client';

import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type ComponentType,
  type ReactNode,
} from 'react';
import { useQuery } from '@tanstack/react-query';
import { Clock, Pencil } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { Input } from '@goosar/ui/components/ui/input';
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupSelectTrigger,
  InputGroupText,
  InputGroupTimeInput,
} from '@goosar/ui/components/ui/input-group';
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@goosar/ui/components/ui/select';
import { TimeInput } from '@goosar/ui/components/ui/time-input';
import { cronPreviewOptions } from '@goosar/core/autopilots/queries';
import { ApiError } from '@goosar/core/api';
import { timezoneOptions } from '../../../common/timezone-select';
import { useDebouncedValue } from '../../../common/use-debounced-value';
import { SegmentedToggle } from '../../../common/segmented-toggle';
import { formatInTimeZone } from '../../../common/format-in-time-zone';
import { TimezonePicker } from '../pickers/timezone-picker';
import { useT } from '../../../i18n';
import type { DayPattern, ScheduleConfig, TimePattern } from './model';
import { DAY_KEYS, pad2, timeParts } from './model';
import {
  cronFields,
  extractTimezonePrefix,
  hasTimezonePrefix,
  parseCron,
  toCron,
} from './cron-mapping';
import { classifyScheduleRejection } from './validate';
import { useDescribeSchedule } from './describe';

export interface ScheduleEditorProps {
  value: ScheduleConfig;
  onChange: (value: ScheduleConfig) => void;
  wsId: string;
  disabled?: boolean;
  disabledReason?: string;
  onValidityChange?: (valid: boolean) => void;
}

const PREVIEW_DEBOUNCE_MS = 300;

type EveryPattern = Extract<TimePattern, { kind: 'every' }>;
type ScheduleWindow = { from: string; to: string };

function useNowTicker(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function useFormatCountdown() {
  const { t } = useT('autopilots');
  return (target: Date, now: Date): string => {
    const diffMs = target.getTime() - now.getTime();
    if (diffMs < 60_000) return t(($) => $.schedule_editor.countdown.less_than_minute);
    const minutes = Math.floor(diffMs / 60_000);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    if (days > 0)
      return t(($) => $.schedule_editor.countdown.days_hours, {
        days,
        hours: hours % 24,
        minutes: minutes % 60,
      });
    if (hours > 0)
      return t(($) => $.schedule_editor.countdown.hours_minutes, {
        hours,
        minutes: minutes % 60,
      });
    return t(($) => $.schedule_editor.countdown.minutes, { minutes });
  };
}

function isFullDay(window: ScheduleWindow): boolean {
  return timeParts(window.from).hour === 0 && timeParts(window.to).hour === 23;
}

function displayWindow(time: EveryPattern): ScheduleWindow {
  if (time.window !== null) return time.window;
  return time.unit === 'hours'
    ? { from: `00:${pad2(time.minute)}`, to: `23:${pad2(time.minute)}` }
    : { from: '00:00', to: '23:59' };
}

function timeAnchorOf(time: TimePattern): string | null {
  if (time.kind === 'at') return time.time;
  if (time.window === null) return null;
  return `${pad2(timeParts(time.window.from).hour)}:${pad2(time.minute)}`;
}

function defaultAtTime(prev: TimePattern, anchor: string): string {
  return (
    timeAnchorOf(prev) ??
    `${pad2(timeParts(anchor).hour)}:${pad2(prev.kind === 'every' ? prev.minute : 0)}`
  );
}

function defaultEveryTime(prev: TimePattern, anchor: EveryPattern | null): EveryPattern {
  if (prev.kind === 'every') return prev;
  const { hour, minute } = timeParts(prev.time);
  const base: EveryPattern = anchor ?? {
    kind: 'every',
    interval: 1,
    unit: 'hours',
    window: null,
    minute,
  };
  const time: EveryPattern = { ...base, minute };
  if (time.window === null) return time;
  const rebased =
    hour < timeParts(time.window.to).hour
      ? { from: `${pad2(hour)}:${pad2(minute)}`, to: time.window.to }
      : time.window;
  const window = clampWindow(rebased, time.unit, minute);
  return { ...time, window: isFullDay(window) ? null : window };
}

function clampWindow(
  window: ScheduleWindow,
  unit: EveryPattern['unit'],
  minute: number,
): ScheduleWindow {
  const from = timeParts(window.from);
  const to = timeParts(window.to);
  const edgeMinute = unit === 'hours' ? minute : 0;
  const endMinute = unit === 'hours' ? minute : 59;
  const endHour = Math.max(to.hour, from.hour);
  return {
    from: `${pad2(from.hour)}:${pad2(edgeMinute)}`,
    to: `${pad2(endHour)}:${pad2(endMinute)}`,
  };
}

function toggleDay(days: number[], day: number): number[] {
  if (days.includes(day)) {
    if (days.length === 1) return days;
    return days.filter((d) => d !== day);
  }
  return [...days, day].toSorted((a, b) => a - b);
}

function ScheduleField({
  label,
  disabled,
  children,
}: {
  label: string;
  disabled: boolean;
  children: ReactNode;
}) {
  return (
    <fieldset
      disabled={disabled}
      className={cn('flex min-w-0 flex-col gap-1.5 border-0 p-0', disabled && 'opacity-60')}
    >
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      {/* flex gap, not space-y: Base UI's Select appends a hidden fixed-position
          form input after the trigger, which space-y counts as the last child —
          handing its 8px to the visible control and inflating the block. */}
      <div className="flex min-w-0 flex-col gap-2">{children}</div>
    </fieldset>
  );
}

function NumberField({
  value,
  min,
  max,
  onCommit,
  className,
  ariaLabel,
  autoFocus,
  component: Field = Input,
}: {
  value: number;
  min: number;
  max: number;
  onCommit: (value: number) => void;
  className?: string;
  ariaLabel: string;
  autoFocus?: boolean;
  component?: ComponentType<ComponentProps<'input'>>;
}) {
  const [text, setText] = useState(String(value));
  const lastValueRef = useRef(value);
  if (lastValueRef.current !== value) {
    lastValueRef.current = value;
    setText(String(value));
  }
  return (
    <Field
      type="number"
      aria-label={ariaLabel}
      autoFocus={autoFocus}
      min={min}
      max={max}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        const n = parseInt(e.target.value, 10);
        if (!Number.isNaN(n) && n >= min && n <= max) onCommit(n);
      }}
      onKeyDown={(e) => {
        if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
        e.preventDefault();
        const typed = parseInt(text, 10);
        const stepped =
          Number.isNaN(typed) || (typed >= min && typed <= max)
            ? (Number.isNaN(typed) ? value : typed) + (e.key === 'ArrowUp' ? 1 : -1)
            : Math.min(max, Math.max(min, typed));
        const wrapped = stepped > max ? min : stepped < min ? max : stepped;
        setText(String(wrapped));
        if (wrapped !== lastValueRef.current) onCommit(wrapped);
      }}
      onBlur={() => {
        const n = parseInt(text, 10);
        if (Number.isNaN(n)) {
          setText(String(lastValueRef.current));
          return;
        }
        const clamped = Math.min(max, Math.max(min, n));
        setText(String(clamped));
        if (clamped !== lastValueRef.current) onCommit(clamped);
      }}
      className={className}
    />
  );
}

const WINDOW_FIELD_COMPACT = 'gap-0.5 px-1';

export function ScheduleEditor({
  value,
  onChange,
  wsId,
  disabled,
  disabledReason,
  onValidityChange,
}: ScheduleEditorProps) {
  const { t, i18n } = useT('autopilots');
  const describe = useDescribeSchedule();
  const formatCountdown = useFormatCountdown();
  const now = useNowTicker();
  const advanced = value.raw !== null;
  const locked = disabled === true;

  const [cronDraft, setCronDraft] = useState<string | null>(null);
  const committedCron = toCron(value);
  const fieldsText = cronFields(value);
  const cronText = cronDraft ?? fieldsText;
  const ownPrefix = hasTimezonePrefix(cronText);
  const [cronEditing, setCronEditing] = useState(false);
  const cronInputRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    if (!cronEditing) return;
    const input = cronInputRef.current;
    if (input === null) return;
    input.setSelectionRange(0, 0);
    input.scrollLeft = 0;
  }, [cronEditing]);
  const cronErrorId = useId();
  const cronOpen = advanced || cronEditing;

  const timeAnchorRef = useRef(timeAnchorOf(value.time) ?? '09:00');
  const currentAnchor = timeAnchorOf(value.time);
  if (currentAnchor !== null) timeAnchorRef.current = currentAnchor;

  const everyAnchorRef = useRef<EveryPattern | null>(null);
  if (value.time.kind === 'every') everyAnchorRef.current = value.time;

  const intervalAnchorRef = useRef<Record<EveryPattern['unit'], number | null>>({
    hours: null,
    minutes: null,
  });
  if (value.time.kind === 'every') {
    intervalAnchorRef.current[value.time.unit] = value.time.interval;
  }

  const daysOfWeekAnchorRef = useRef<number[]>([1]);
  const dayOfMonthAnchorRef = useRef(1);
  if (value.days.kind === 'weekly') daysOfWeekAnchorRef.current = value.days.daysOfWeek;
  if (value.days.kind === 'monthly') dayOfMonthAnchorRef.current = value.days.dayOfMonth;

  const focusTimeFieldRef = useRef(false);
  const focusDayFieldRef = useRef(false);
  useEffect(() => {
    focusTimeFieldRef.current = false;
    focusDayFieldRef.current = false;
  });

  const pendingTzPromotionRef = useRef<string | null>(null);

  const applyDraft = (draft: string): boolean => {
    const next = draft.trim();
    setCronDraft(null);
    if (next.length === 0 || next === cronFields(value)) return advanced;
    if (extractTimezonePrefix(next) !== null) {
      pendingTzPromotionRef.current = next;
      onChange({ ...value, raw: next });
      return true;
    }
    pendingTzPromotionRef.current = null;
    const parsed = parseCron(next, value.timezone);
    onChange(parsed.raw === null ? parsed : { ...value, raw: next });
    return parsed.raw !== null;
  };

  const setTime = (time: TimePattern) => {
    setCronDraft(null);
    onChange({ ...value, time, raw: null });
  };
  const setDays = (days: DayPattern) => {
    setCronDraft(null);
    onChange({ ...value, days, raw: null });
  };
  const setTimezone = (timezone: string) => onChange({ ...value, timezone });

  const previewExpr = useDebouncedValue(committedCron, PREVIEW_DEBOUNCE_MS);
  const preview = useQuery(
    cronPreviewOptions(wsId, previewExpr, value.timezone, {
      enabled: previewExpr.trim().length > 0,
    }),
  );
  const { refetch } = preview;
  const nextRuns = preview.data?.next_runs ?? null;

  const previewIsCurrent = previewExpr === committedCron;
  const liveRejection =
    previewIsCurrent && preview.error instanceof ApiError && preview.error.status === 400
      ? preview.error
      : null;

  const promoted = pendingTzPromotionRef.current;
  useEffect(() => {
    if (promoted === null || promoted !== committedCron) return;
    if (!previewIsCurrent || !preview.isSuccess) return;
    pendingTzPromotionRef.current = null;
    onChange(parseCron(promoted, value.timezone));
  }, [promoted, committedCron, previewIsCurrent, preview.isSuccess, value.timezone, onChange]);
  const committedKey = `${committedCron} ${value.timezone}`;
  const verdictRef = useRef<{
    key: string;
    rejection: ApiError | null;
    accepted: boolean;
  } | null>(null);
  if (previewIsCurrent) {
    if (liveRejection !== null) {
      verdictRef.current = { key: committedKey, rejection: liveRejection, accepted: false };
    } else if (preview.isSuccess) {
      verdictRef.current = { key: committedKey, rejection: null, accepted: nextRuns !== null };
    }
  }
  const verdict = verdictRef.current?.key === committedKey ? verdictRef.current : null;
  const rejection = liveRejection ?? verdict?.rejection ?? null;
  const scheduleRejection = rejection !== null ? classifyScheduleRejection(rejection) : null;
  const cronErrorDetail = scheduleRejection?.detail ?? null;
  const previewUnavailable =
    previewIsCurrent &&
    ((preview.error !== null && cronErrorDetail === null) ||
      (preview.isSuccess && nextRuns === null));

  const previewIsSettled = previewIsCurrent && preview.isSuccess && nextRuns !== null;
  const serverAccepted = verdict?.accepted === true;
  const shownPreviewRef = useRef<{ runs: string[]; timezone: string } | null>(null);
  if (
    previewIsSettled &&
    (shownPreviewRef.current?.runs !== nextRuns ||
      shownPreviewRef.current.timezone !== value.timezone)
  ) {
    shownPreviewRef.current = { runs: nextRuns, timezone: value.timezone };
  }
  const shownPreview = shownPreviewRef.current;
  const shownRuns = useMemo(() => {
    if (shownPreview === null) return [];
    return shownPreview.runs.map((iso) => ({
      iso,
      label: formatInTimeZone(iso, shownPreview.timezone, i18n.language),
      at: Date.parse(iso),
    }));
  }, [shownPreview, i18n.language]);
  const previewIsPending = !previewIsSettled;
  const previewShowsList =
    cronErrorDetail === null &&
    !previewUnavailable &&
    shownPreview !== null &&
    shownPreview.runs.length > 0;

  const firstRunMs = nextRuns?.[0] !== undefined ? Date.parse(nextRuns[0]) : Number.NaN;

  const queriedKey = `${previewExpr} ${value.timezone}`;
  const refetchedForRef = useRef<string | null>(null);
  useEffect(() => {
    if (Number.isNaN(firstRunMs) || firstRunMs > now.getTime()) return;
    const fetchedFor = `${queriedKey} ${firstRunMs}`;
    if (refetchedForRef.current === fetchedFor) return;
    refetchedForRef.current = fetchedFor;
    void refetch();
  }, [firstRunMs, now, refetch, queriedKey]);

  useEffect(() => {
    onValidityChange?.(cronErrorDetail === null);
  }, [cronErrorDetail, committedCron, value.timezone, onValidityChange]);

  const timezones = useMemo(() => timezoneOptions(value.timezone), [value.timezone]);

  const description = describe(value);

  const dayKindLabel = (kind: DayPattern['kind']): string => {
    if (kind === 'every') return t(($) => $.schedule_editor.days_every);
    if (kind === 'weekly') return t(($) => $.schedule_editor.days_weekly);
    return t(($) => $.schedule_editor.days_monthly);
  };
  const dayKindItems = (['every', 'weekly', 'monthly'] as const).map((kind) => ({
    value: kind,
    label: dayKindLabel(kind),
  }));

  return (
    <div className="space-y-5">
      <ScheduleField label={t(($) => $.schedule_editor.time_label)} disabled={locked || advanced}>
        <SegmentedToggle
          value={value.time.kind}
          options={[
            ['at', t(($) => $.schedule_editor.time_at)],
            ['every', t(($) => $.schedule_editor.time_every)],
          ]}
          onChange={(kind) => {
            focusTimeFieldRef.current = true;
            setTime(
              kind === 'at'
                ? {
                    kind: 'at',
                    time: defaultAtTime(value.time, timeAnchorRef.current),
                  }
                : defaultEveryTime(value.time, everyAnchorRef.current),
            );
          }}
        />
        {value.time.kind === 'at' ? (
          <TimeInput
            value={value.time.time}
            hourLabel={t(($) => $.schedule_editor.a11y.fixed_hour)}
            minuteLabel={t(($) => $.schedule_editor.a11y.fixed_minute)}
            onChange={(v) => setTime({ kind: 'at', time: v })}
            autoFocus={focusTimeFieldRef.current}
          />
        ) : (
          <EveryTimeControls
            time={value.time}
            intervalAnchor={intervalAnchorRef.current}
            onSet={setTime}
            autoFocus={focusTimeFieldRef.current}
          />
        )}
      </ScheduleField>

      <ScheduleField label={t(($) => $.schedule_editor.days_label)} disabled={locked || advanced}>
        {/* A 7:4 grid, the same template as the interval row above, so the two
            rows line up as one two-column grid at the minutes unit. Grid tracks
            size by the template alone — unlike flex, which let each box's own
            padding and w-fit/w-full skew the split and knocked the columns out of
            line. When there is no day-of-month box the select spans both tracks,
            so every/weekly still fill the width. */}
        <div className="grid grid-cols-[7fr_4fr] items-center gap-2">
          <Select
            items={dayKindItems}
            value={value.days.kind}
            onValueChange={(v) => {
              if (!v || v === value.days.kind) return;
              if (v === 'every') setDays({ kind: 'every' });
              else if (v === 'weekly')
                setDays({ kind: 'weekly', daysOfWeek: daysOfWeekAnchorRef.current });
              else {
                focusDayFieldRef.current = true;
                setDays({ kind: 'monthly', dayOfMonth: dayOfMonthAnchorRef.current });
              }
            }}
          >
            <SelectTrigger
              aria-label={t(($) => $.schedule_editor.a11y.day_pattern)}
              className={cn('w-full min-w-0', value.days.kind !== 'monthly' && 'col-span-2')}
            >
              <SelectValue>{dayKindLabel(value.days.kind)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {dayKindItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {value.days.kind === 'monthly' && (
            <NumberField
              value={value.days.dayOfMonth}
              min={1}
              max={31}
              ariaLabel={t(($) => $.schedule_editor.a11y.day_of_month)}
              onCommit={(n) => setDays({ kind: 'monthly', dayOfMonth: n })}
              className="h-8 min-w-0"
              autoFocus={focusDayFieldRef.current}
            />
          )}
        </div>
        {value.days.kind === 'weekly' && (
          <div className="flex w-full gap-0.5">
            {DAY_KEYS.map((dayKey, i) => {
              const days = value.days;
              const selected = days.kind === 'weekly' && days.daysOfWeek.includes(i);
              return (
                <button
                  key={dayKey}
                  type="button"
                  aria-pressed={selected}
                  aria-label={t(($) => $.schedule_editor.a11y.days_full[dayKey])}
                  onClick={() => {
                    if (days.kind !== 'weekly') return;
                    setDays({
                      kind: 'weekly',
                      daysOfWeek: toggleDay(days.daysOfWeek, i),
                    });
                  }}
                  className={cn(
                    'inline-flex h-6.5 min-w-0 flex-1 items-center justify-center rounded-md',
                    'text-[11px] font-medium leading-none transition-colors',
                    selected
                      ? 'bg-foreground text-background'
                      : 'bg-muted text-muted-foreground hover:text-foreground',
                  )}
                >
                  {t(($) => $.schedule_editor.days_short[dayKey])}
                </button>
              );
            })}
          </div>
        )}
        {value.days.kind === 'monthly' && value.days.dayOfMonth >= 29 && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.schedule_editor.monthly_short_month_hint, {
              day: value.days.dayOfMonth,
            })}
          </p>
        )}
      </ScheduleField>

      <ScheduleField label={t(($) => $.schedule_editor.timezone_label)} disabled={locked}>
        {/* The fieldset already disables the trigger; disabled:opacity-100 keeps
            the control from dimming a second time on top of it. */}
        <TimezonePicker
          value={value.timezone}
          onChange={setTimezone}
          options={timezones}
          ariaLabel={t(($) => $.schedule_editor.a11y.timezone)}
          className="disabled:opacity-100"
        />
      </ScheduleField>

      {/* The cron text, the plain-language readback and the server preview are
          all derived views of the same expression, so they share one panel: the
          result of the fields above, not more fields alongside them. The cron
          line inside it doubles as the advanced editing entry — clicking it
          swaps the text for an input, and the form dims while raw mode holds. */}
      <div className="rounded-md bg-muted/40 p-2.5 text-xs text-muted-foreground">
        <div className="space-y-2">
          {/* The plain-language sentence leads: it is the line a person reads,
              so it takes the panel's entry and the foreground color, and the
              expression drops to a technical echo below it. */}
          {description !== null && (
            <p className="flex items-start gap-1.5 text-foreground">
              <Clock className="mt-0.5 h-3 w-3 shrink-0" />
              <span>{description}</span>
            </p>
          )}
          {cronOpen ? (
            <InputGroup className="bg-background dark:bg-input/30">
              {!ownPrefix && (
                <InputGroupAddon align="block-start" className="font-mono text-xs">
                  {/* eslint-disable-next-line i18next/no-literal-string -- cron syntax, not copy */}
                  <span className="min-w-0 truncate">TZ={value.timezone}</span>
                </InputGroupAddon>
              )}
              <InputGroupInput
                ref={cronInputRef}
                type="text"
                autoFocus={cronEditing}
                aria-label={t(($) => $.schedule_editor.cron_toggle)}
                value={cronText}
                disabled={locked}
                onChange={(e) => setCronDraft(e.target.value)}
                onBlur={() => {
                  const stillAdvanced = cronDraft !== null ? applyDraft(cronDraft) : advanced;
                  if (!stillAdvanced) setCronEditing(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && cronDraft !== null) {
                    e.preventDefault();
                    setCronEditing(true);
                    applyDraft(cronDraft);
                  }
                }}
                aria-invalid={cronErrorDetail !== null}
                aria-describedby={cronErrorDetail !== null ? cronErrorId : undefined}
                className="font-mono text-sm"
              />
            </InputGroup>
          ) : (
            <button
              type="button"
              disabled={locked}
              onClick={() => setCronEditing(true)}
              aria-label={t(($) => $.schedule_editor.cron_click_to_edit)}
              className="flex w-full items-center gap-1.5 text-left hover:text-foreground disabled:pointer-events-none disabled:opacity-60"
            >
              {/* Closed, the readback shows only the editable text — the zone
                  is already on screen in the picker above, and a self-prefixed
                  advanced expression carries its zone in the text itself. The
                  full wire form appears when the field opens, as the fixed
                  segment above the fields. */}
              <span className="min-w-0 truncate font-mono">{cronText}</span>
              <Pencil aria-hidden className="h-3 w-3 shrink-0 opacity-60" />
            </button>
          )}
          {/* One note under the expression, never a stack of them: a rejection is
              what the user must act on, the advanced notice explains why the
              controls are off, and the syntax hint is the fallback. */}
          {cronErrorDetail !== null ? (
            <div id={cronErrorId} role="alert" className="space-y-0.5">
              <p className="text-destructive">
                {scheduleRejection?.code === 'invalid_timezone'
                  ? t(($) => $.schedule_editor.timezone_invalid)
                  : t(($) => $.schedule_editor.cron_invalid)}
              </p>
              {/* The parser's own words, verbatim — untranslated, but it is the
                  only text that says which field is wrong. */}
              <p className="font-mono text-[11px] text-destructive/70">{cronErrorDetail}</p>
            </div>
          ) : advanced ? (
            <p>
              {serverAccepted
                ? t(($) => $.schedule_editor.advanced_hint)
                : previewUnavailable
                  ? t(($) => $.schedule_editor.advanced_unverified)
                  : t(($) => $.schedule_editor.advanced_checking)}
            </p>
          ) : cronOpen ? (
            <p>{t(($) => $.schedule_editor.cron_hint)}</p>
          ) : null}
        </div>

        {/* A rejected expression has nothing to preview: the whole section goes,
            heading and divider with it, rather than leaving an empty frame under
            the error. The error already fills the space it vacates.

            Otherwise the height is reserved: the preview is a round trip behind
            every edit, and a section that collapsed and reappeared would reflow
            the dialog. Same elements across fetches: React swaps the text nodes
            instead of tearing the section down, so a re-render of the same
            schedule never flashes — it just dims and reports busy until the
            answer is current. */}
        {cronErrorDetail === null && (
          <div
            aria-busy={previewShowsList ? previewIsPending : undefined}
            className={cn(
              'mt-2.5 min-h-14 border-t border-border/60 pt-2.5 transition-opacity',
              previewShowsList && previewIsPending && 'opacity-50',
            )}
          >
            <p className="mb-1.5 font-medium text-foreground">
              {t(($) => $.schedule_editor.next_runs_label)}
            </p>
            {previewUnavailable ? (
              <p>{t(($) => $.schedule_editor.preview_unavailable)}</p>
            ) : shownPreview !== null && shownPreview.runs.length > 0 ? (
              <ul className="grid grid-cols-[max-content_max-content] gap-x-5 gap-y-1">
                {shownRuns.map(({ iso, label, at }) => (
                  <li key={iso} className="contents">
                    {/* Dark date, dim countdown: the icon column is gone — the
                      grid already lines the rows up, and the describe line's
                      clock stays the panel's only icon. */}
                    <span className="text-foreground tabular-nums">{label}</span>
                    {/* Each run carries its own countdown, next to the time
                      it counts down to. */}
                    <span className="whitespace-nowrap tabular-nums opacity-70">
                      {Number.isNaN(at)
                        ? ''
                        : t(($) => $.schedule_editor.next_in, {
                            countdown: formatCountdown(new Date(at), now),
                          })}
                    </span>
                  </li>
                ))}
              </ul>
            ) : previewIsSettled ? (
              <p>{t(($) => $.schedule_editor.no_upcoming_runs)}</p>
            ) : null}
          </div>
        )}
      </div>
      {disabled === true && disabledReason !== undefined && (
        <p className="mt-2 text-[11px] text-muted-foreground">{disabledReason}</p>
      )}
    </div>
  );
}

function EveryTimeControls({
  time,
  intervalAnchor,
  onSet,
  autoFocus,
}: {
  time: EveryPattern;
  intervalAnchor: Record<EveryPattern['unit'], number | null>;
  onSet: (time: TimePattern) => void;
  autoFocus?: boolean;
}) {
  const { t } = useT('autopilots');
  const maxInterval = time.unit === 'hours' ? 23 : 59;
  const unitItems = (['hours', 'minutes'] as const).map((unit) => ({
    value: unit,
    label:
      unit === 'hours'
        ? t(($) => $.schedule_editor.unit_hours)
        : t(($) => $.schedule_editor.unit_minutes),
  }));
  const window = displayWindow(time);

  const groupRef = useRef<HTMLDivElement>(null);
  const focusStepOnUnitChangeRef = useRef(false);
  useEffect(() => {
    if (!focusStepOnUnitChangeRef.current) return;
    focusStepOnUnitChangeRef.current = false;
    const step = groupRef.current?.querySelector<HTMLInputElement>('input[type="number"]');
    step?.focus();
    step?.select();
  }, [time.unit]);

  const endAnchorRef = useRef<number | null>(null);
  const draggedEndRef = useRef<number | null>(null);
  if (timeParts(window.to).hour !== draggedEndRef.current) {
    endAnchorRef.current = timeParts(window.to).hour;
  }
  const anchoredEnd = (from: string, to: string): string => {
    const end = Math.max(endAnchorRef.current ?? timeParts(to).hour, timeParts(from).hour);
    return `${pad2(end)}:${pad2(timeParts(to).minute)}`;
  };

  const setWindow = (next: ScheduleWindow, minute: number) => {
    const clamped = clampWindow(next, time.unit, minute);
    onSet({ ...time, minute, window: isFullDay(clamped) ? null : clamped });
  };

  return (
    <div ref={groupRef} className="grid min-w-0 grid-cols-[7fr_4fr] items-center gap-2">
      {/* "Every 3 hours" is one setting, so it reads as one control: the prefix,
          the step and the unit share a single box. The step takes the room the
          other two leave — the prefix is as wide as its translation, the unit as
          wide as its longest option — so the field never has to be sized against
          text it cannot see. */}
      <InputGroup className="min-w-0">
        {/* pl-2.5, not the addon's default pl-2: this row sits directly above the
            day select, and the two read as one column only if their first
            character starts at the same x. A Select trigger pads to 2.5. */}
        <InputGroupAddon className="pl-2.5">
          {t(($) => $.schedule_editor.every_prefix)}
        </InputGroupAddon>
        <NumberField
          component={InputGroupInput}
          autoFocus={autoFocus}
          value={time.interval}
          min={1}
          max={maxInterval}
          ariaLabel={t(($) => $.schedule_editor.a11y.interval)}
          onCommit={(n) => onSet({ ...time, interval: n })}
          className="min-w-[2.5ch] px-0 text-center [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
        />
        <Select
          items={unitItems}
          value={time.unit}
          onValueChange={(v) => {
            if (!v || v === time.unit) return;
            const unit = v as EveryPattern['unit'];
            const max = unit === 'hours' ? 23 : 59;
            focusStepOnUnitChangeRef.current = true;
            onSet({
              ...time,
              unit,
              interval: intervalAnchor[unit] ?? Math.min(time.interval, max),
              window: time.window === null ? null : clampWindow(time.window, unit, time.minute),
            });
          }}
        >
          <InputGroupSelectTrigger
            aria-label={t(($) => $.schedule_editor.a11y.interval_unit)}
            className="pr-2.5"
          >
            <SelectValue>{unitItems.find((item) => item.value === time.unit)?.label}</SelectValue>
          </InputGroupSelectTrigger>
          {/* min-w-[7rem]: below the component's 144px default, which the two
              short options don't need — but not so far that "minutes" and its
              check mark (the item's pr-8) lose room. */}
          <SelectContent className="min-w-[7rem]">
            {unitItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </InputGroup>
      {/* The window, always on screen. One clock leads the whole box (an addon,
          like the interval row's "Every"), not one per field: a single icon marks
          this as a time control without the width two would cost between the ends.

          A minute-step window is hour-granular — cron puts the step in the minute
          field, so the bounds carry no minute of their own. Both ends are the same
          in-place field either way; the minute segment is simply not offered where
          it would mean nothing. */}
      {/* Sits in the grid's 4-share second column, under the day row's number.
          min-w-fit lets that track grow past the share to the box's content: at
          the minutes unit the hour-only window fits the share and stays aligned;
          at hours the wider HH:MM ~ HH:MM pushes the track out and the interval
          yields, rather than the window clipping (its segments never shrink).
          has-disabled off: the group dims itself when anything inside it is
          disabled, and the minute segments are, by design, under a minute step —
          which does not make the row unavailable. A locked or advanced schedule
          still greys out, from the fieldset around the whole block. */}
      <InputGroup className="min-w-fit has-disabled:bg-transparent has-disabled:opacity-100 dark:has-disabled:bg-input/30">
        {/* Leads the box, not focusable: the fields after it take the focus and
            the group's border. pr-0 lets the start field's own padding set the
            icon-to-digit gap, so it matches the gap between the two ends. */}
        <InputGroupAddon className="pl-2 pr-0">
          <Clock className="size-3.5 text-muted-foreground" />
        </InputGroupAddon>
        <InputGroupTimeInput
          className={WINDOW_FIELD_COMPACT}
          showIcon={false}
          hourOnly={time.unit === 'minutes'}
          hourLabel={t(($) => $.schedule_editor.a11y.window_start_hour)}
          minuteLabel={t(($) => $.schedule_editor.a11y.window_start_minute)}
          value={window.from}
          onChange={(v) => {
            const minute = time.unit === 'hours' ? timeParts(v).minute : time.minute;
            const to = anchoredEnd(v, window.to);
            draggedEndRef.current = timeParts(to).hour;
            setWindow({ from: v, to }, minute);
          }}
        />
        <InputGroupText className="shrink-0">~</InputGroupText>
        {/* The window's end. The two ends constrain each other as they are edited
            — hourMin keeps the end from falling below the start — so a reversed
            window, which has no cron form, cannot even be displayed, let alone
            submitted. The bound is the field's own range rather than a clamp
            applied to whatever it emits: the arrow keys wrap inside it (stepping
            below the start hour lands on 23, not on a key that does nothing).

            Clearing it opens the window to the end of the day (hourClearTo)
            rather than collapsing it onto the start: backspace on the end of a
            window is how a keyboard user says "run until the day is out". */}
        <InputGroupTimeInput
          className={WINDOW_FIELD_COMPACT}
          showIcon={false}
          value={window.to}
          hourMin={timeParts(window.from).hour}
          hourClearTo={23}
          hourOnly={time.unit === 'minutes'}
          hourLabel={t(($) => $.schedule_editor.a11y.window_end_hour)}
          minuteLabel={t(($) => $.schedule_editor.a11y.window_end_minute)}
          onChange={(v) => {
            const minute = time.unit === 'hours' ? timeParts(v).minute : time.minute;
            endAnchorRef.current = timeParts(v).hour;
            draggedEndRef.current = timeParts(v).hour;
            setWindow({ from: window.from, to: v }, minute);
          }}
        />
      </InputGroup>
    </div>
  );
}
