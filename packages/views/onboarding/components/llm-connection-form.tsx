'use client';

import { useCallback, useMemo, useState, type ReactNode } from 'react';
import {
  Check,
  ChevronDown,
  ChevronUp,
  Eye,
  EyeOff,
  Info,
  Loader2,
  TriangleAlert,
} from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { cn } from '@goosar/ui/lib/utils';
import { llmConnectionPresets, type LlmConnectionChoice } from '../presets';
import { useConfigStore } from '@goosar/core/config';
import { useT } from '../../i18n';

export interface LlmConnectionValues {
  apiBase: string;
  model: string;
  apiKey: string;
}

export type LlmConnectionSaveResult =
  | {
      ok: true;
      ineffective?: boolean;
    }
  | { ok: false; message?: string };

export type SaveLlmConnection = (values: LlmConnectionValues) => Promise<LlmConnectionSaveResult>;

export interface LlmServerConnectionInfo {
  origin: string;
  locked: boolean;
  hasApiKey?: boolean;
  baseUrl?: string;
  model?: string;
}

type Status =
  | { kind: 'idle' }
  | { kind: 'saving' }
  | { kind: 'saved' }
  | { kind: 'ineffective' }
  | { kind: 'error'; message: string };

export interface LlmExistingConnectionInfo {
  apiBase: string;
  model: string;
  hasKey: boolean;
}

export function LlmConnectionForm({
  onSave,
  className,
  defaultChoice,
  perimeterCaMissing,
  serverConnection,
  existingConnection,
}: {
  onSave: SaveLlmConnection;
  className?: string;
  defaultChoice?: LlmConnectionChoice;
  perimeterCaMissing?: boolean;
  serverConnection?: LlmServerConnectionInfo | null;
  existingConnection?: LlmExistingConnectionInfo | null;
}) {
  const { t } = useT('onboarding');

  const deploymentHosts = useConfigStore((s) => s.deploymentHosts);
  const presets = useMemo(() => llmConnectionPresets(deploymentHosts), [deploymentHosts]);
  const [choice, setChoice] = useState<LlmConnectionChoice>(() =>
    defaultChoice === 'custom' ||
    (defaultChoice !== undefined && presets.some((preset) => preset.id === defaultChoice))
      ? defaultChoice
      : presets[0]!.id,
  );
  const [apiBase, setApiBase] = useState('');
  const [model, setModel] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [keyVisible, setKeyVisible] = useState(false);
  const [replacing, setReplacing] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });
  const [keyPresetHint, setKeyPresetHint] = useState(false);
  const [guideOpen, setGuideOpen] = useState(false);

  const isCustom = choice === 'custom';

  const selectChoice = useCallback(
    (next: LlmConnectionChoice) => {
      if (next === choice) return;
      setKeyPresetHint(next !== 'custom' && choice !== 'custom' && apiKey.trim().length > 0);
      setChoice(next);
    },
    [choice, apiKey],
  );

  const values = useMemo<LlmConnectionValues>(() => {
    const key = apiKey.trim();
    const preset = choice === 'custom' ? null : presets.find((entry) => entry.id === choice);
    if (preset) {
      return { apiBase: preset.apiBase, model: preset.model, apiKey: key };
    }
    return { apiBase: apiBase.trim(), model: model.trim(), apiKey: key };
  }, [choice, apiBase, model, apiKey, presets]);

  const complete = values.apiBase.length > 0 && values.model.length > 0 && values.apiKey.length > 0;
  const saving = status.kind === 'saving';

  const save = useCallback(async () => {
    if (!complete || saving) return;
    setStatus({ kind: 'saving' });
    try {
      const result = await onSave(values);
      if (result.ok) {
        setApiKey('');
        setKeyVisible(false);
        setKeyPresetHint(false);
        setStatus({ kind: result.ineffective === true ? 'ineffective' : 'saved' });
        return;
      }
      const message = result.message ?? t(($) => $.step_runtime.llm.error_generic);
      setStatus({ kind: 'error', message });
      toast.error(message);
    } catch (err) {
      const message = t(($) => $.step_runtime.llm.error_generic);
      setStatus({ kind: 'error', message });
      toast.error(message);
      void err;
    }
  }, [complete, saving, onSave, values, t]);

  const presetLabel = (id: LlmConnectionChoice): string => {
    switch (id) {
      case 'perimeter':
        return t(($) => $.step_runtime.llm.preset_perimeter);
      case 'outside':
        return t(($) => $.step_runtime.llm.preset_outside);
      case 'custom':
        return t(($) => $.step_runtime.llm.preset_custom);
    }
  };

  const presetGuide = (id: LlmConnectionChoice): string => {
    switch (id) {
      case 'perimeter':
        return t(($) => $.step_runtime.llm.guide_perimeter);
      case 'outside':
        return t(($) => $.step_runtime.llm.guide_outside);
      case 'custom':
        return t(($) => $.step_runtime.llm.guide_custom);
    }
  };

  const choices: LlmConnectionChoice[] = [...presets.map((preset) => preset.id), 'custom'];

  const serverSuppliesKey =
    serverConnection?.hasApiKey === true && serverConnection.locked === true;

  if (serverSuppliesKey) {
    return (
      <section
        className={cn('rounded-lg border bg-card p-5', className)}
        aria-labelledby="llm-connection-title"
      >
        <h2 id="llm-connection-title" className="text-[14.5px] font-medium text-foreground">
          {t(($) => $.step_runtime.llm.server_key_title)}
        </h2>
        <p className="mt-1.5 flex items-start gap-1.5 text-[12.5px] leading-[1.55] text-muted-foreground">
          <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" aria-hidden />
          {t(($) => $.step_runtime.llm.server_key_body)}
        </p>
      </section>
    );
  }

  if (existingConnection && !replacing) {
    return (
      <section
        className={cn('rounded-lg border bg-card p-5', className)}
        aria-labelledby="llm-connection-title"
      >
        <h2 id="llm-connection-title" className="text-[14.5px] font-medium text-foreground">
          {t(($) => $.step_runtime.llm.existing_title)}
        </h2>
        <p className="mt-1 text-[13px] text-muted-foreground">
          {t(($) => $.step_runtime.llm.existing_hint)}
        </p>
        <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[13px]">
          <dt className="text-muted-foreground">{t(($) => $.step_runtime.llm.base_url_label)}</dt>
          <dd className="break-all font-mono text-foreground">{existingConnection.apiBase}</dd>
          <dt className="text-muted-foreground">{t(($) => $.step_runtime.llm.model_label)}</dt>
          <dd className="break-all font-mono text-foreground">{existingConnection.model}</dd>
          <dt className="text-muted-foreground">{t(($) => $.step_runtime.llm.api_key_label)}</dt>
          <dd className="text-foreground">
            {existingConnection.hasKey
              ? t(($) => $.step_runtime.llm.existing_key_set)
              : t(($) => $.step_runtime.llm.existing_key_unset)}
          </dd>
        </dl>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={() => setReplacing(true)}>
            {t(($) => $.step_runtime.llm.existing_change)}
          </Button>
        </div>
        <p className="mt-2 text-[12px] text-muted-foreground">
          {t(($) => $.step_runtime.llm.existing_keep_note)}
        </p>
      </section>
    );
  }

  return (
    <section
      className={cn('rounded-lg border bg-card p-5', className)}
      aria-labelledby="llm-connection-title"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <h2 id="llm-connection-title" className="text-[14.5px] font-medium text-foreground">
          {t(($) => $.step_runtime.llm.title)}
        </h2>
        <span className="rounded-full border px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
          {t(($) => $.step_runtime.llm.optional_badge)}
        </span>
      </div>
      <p className="mt-1.5 text-[12.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.llm.subtitle)}
      </p>

      {/* Origin of the server-provided connection (issue #225). The label
          switch needs a default branch: origins are server-driven and a
          build must render one it has never heard of. Explicit `=== true`
          on `locked` per the defensive-boolean rule. */}
      {serverConnection != null && (
        <p
          data-testid="llm-server-origin"
          className="mt-2.5 flex items-start gap-1.5 rounded-md border bg-muted/40 px-2.5 py-2 text-[11.5px] leading-[1.5] text-muted-foreground"
        >
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
          <span>
            {t(($) => $.step_runtime.llm.server_origin_note, {
              origin: (() => {
                switch (serverConnection.origin) {
                  case 'policy':
                    return t(($) => $.step_runtime.llm.origin_policy);
                  case 'workspace':
                    return t(($) => $.step_runtime.llm.origin_workspace);
                  case 'user_override':
                    return t(($) => $.step_runtime.llm.origin_user_override);
                  case 'machine':
                    return t(($) => $.step_runtime.llm.origin_machine);
                  default:
                    return t(($) => $.step_runtime.llm.origin_server);
                }
              })(),
            })}{' '}
            {serverConnection.locked === true
              ? t(($) => $.step_runtime.llm.server_origin_locked)
              : t(($) => $.step_runtime.llm.server_origin_unlocked)}
          </span>
        </p>
      )}

      <div className="mt-5 flex flex-col gap-4">
        {/* Preset picker — one radio per known endpoint plus the manual
            escape hatch. Selecting a preset hides the address/model fields
            entirely: the user's only job is the key. */}
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-medium text-foreground">
            {t(($) => $.step_runtime.llm.preset_label)}
          </span>
          <div
            role="radiogroup"
            aria-label={t(($) => $.step_runtime.llm.preset_label)}
            className="flex flex-wrap gap-2"
          >
            {choices.map((id) => {
              const active = choice === id;
              return (
                <button
                  key={id}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  disabled={saving}
                  onClick={() => selectChoice(id)}
                  className={cn(
                    'rounded-full border px-3.5 py-1.5 text-[13px] font-medium transition-colors',
                    active
                      ? 'border-foreground bg-foreground text-background'
                      : 'bg-background text-foreground hover:border-foreground/40',
                  )}
                >
                  {presetLabel(id)}
                </button>
              );
            })}
          </div>
          {/* Transparency line: the chosen preset's endpoint and model are
              shown, not hidden, so the values written to the runtime are
              never a mystery. */}
          {!isCustom && (
            <p className="font-mono text-[11.5px] leading-[1.5] text-muted-foreground">
              {values.apiBase}
              {' · '}
              {values.model}
            </p>
          )}
          {/* No-CA fallback (issue #46): without the corporate CA bundle the
              perimeter endpoint's TLS cannot be verified from this machine, so
              saying "saved" here would set the user up for an opaque TLS error
              at the first model call. Warns, never blocks — the values may be
              intended for later, or for after the bundle is installed. */}
          {choice === 'perimeter' && perimeterCaMissing === true && (
            <p
              aria-live="polite"
              className="flex items-start gap-1.5 text-[11.5px] leading-[1.5] text-warning"
            >
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_runtime.llm.ca_missing_hint)}
            </p>
          )}
          {/* Per-preset «Где взять ключ» (issue #438). Collapsed so it never
              competes with the field it explains, and keyed off `choice` so
              switching presets swaps the steps rather than leaving stale ones. */}
          <button
            type="button"
            onClick={() => setGuideOpen((open) => !open)}
            aria-expanded={guideOpen}
            className="mt-1 flex items-center gap-1.5 self-start text-[12.5px] font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            {guideOpen ? (
              <ChevronUp className="h-3.5 w-3.5" aria-hidden />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" aria-hidden />
            )}
            {t(($) => $.step_runtime.llm.guide_toggle)}
          </button>
          {guideOpen && (
            <p
              data-testid="llm-key-guide"
              className="whitespace-pre-line rounded-md bg-muted/40 p-3.5 text-[12.5px] leading-[1.6] text-muted-foreground"
            >
              {presetGuide(choice)}
            </p>
          )}
        </div>

        {isCustom && (
          <>
            <Field
              id="llm-api-base"
              label={t(($) => $.step_runtime.llm.base_url_label)}
              hint={t(($) => $.step_runtime.llm.base_url_hint)}
            >
              <Input
                id="llm-api-base"
                type="text"
                inputMode="url"
                autoComplete="off"
                spellCheck={false}
                disabled={saving}
                placeholder={t(($) => $.step_runtime.llm.base_url_placeholder)}
                value={apiBase}
                onChange={(event) => setApiBase(event.target.value)}
              />
            </Field>

            <Field
              id="llm-model"
              label={t(($) => $.step_runtime.llm.model_label)}
              hint={t(($) => $.step_runtime.llm.model_hint)}
            >
              <Input
                id="llm-model"
                type="text"
                autoComplete="off"
                spellCheck={false}
                disabled={saving}
                placeholder={t(($) => $.step_runtime.llm.model_placeholder)}
                value={model}
                onChange={(event) => setModel(event.target.value)}
              />
            </Field>
          </>
        )}

        <Field
          id="llm-api-key"
          label={t(($) => $.step_runtime.llm.api_key_label)}
          hint={t(($) => $.step_runtime.llm.api_key_hint)}
        >
          {/* Masked by default. The toggle exists because the target user is
              pasting a long opaque string and has no other way to check it
              landed intact — but the field starts hidden and re-hides itself
              after a successful save. */}
          <div className="relative">
            <Input
              id="llm-api-key"
              type={keyVisible ? 'text' : 'password'}
              autoComplete="off"
              spellCheck={false}
              disabled={saving}
              placeholder={t(($) => $.step_runtime.llm.api_key_placeholder)}
              value={apiKey}
              onChange={(event) => {
                setApiKey(event.target.value);
                setKeyPresetHint(false);
              }}
              className="pr-10"
            />
            <button
              type="button"
              onClick={() => setKeyVisible((visible) => !visible)}
              aria-label={
                keyVisible
                  ? t(($) => $.step_runtime.llm.hide_key)
                  : t(($) => $.step_runtime.llm.show_key)
              }
              aria-pressed={keyVisible}
              className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
            >
              {keyVisible ? (
                <EyeOff className="h-3.5 w-3.5" aria-hidden />
              ) : (
                <Eye className="h-3.5 w-3.5" aria-hidden />
              )}
            </button>
          </div>
          {/* Preset-switch reminder (see keyPresetHint above). Rendered inside
              the key field's slot so the warning sits next to the value it is
              about; aria-live because the trigger (a preset radio) is far from
              the field a screen reader user would otherwise never revisit. */}
          {keyPresetHint && (
            <p
              aria-live="polite"
              className="mt-1.5 flex items-start gap-1.5 text-[11.5px] leading-[1.5] text-warning"
            >
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_runtime.llm.key_preset_hint, {
                preset: presetLabel(choice),
              })}
            </p>
          )}
        </Field>
      </div>

      <div className="mt-5 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        {/* One live region for all three outcomes so a screen reader hears the
            result of a save it cannot see. */}
        <div aria-live="polite" className="min-w-0 flex-1">
          {status.kind === 'saved' && (
            <p className="flex items-center gap-1.5 text-[12.5px] text-success">
              <Check className="h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_runtime.llm.saved)}
            </p>
          )}
          {status.kind === 'ineffective' && (
            <p className="flex items-start gap-1.5 text-[12.5px] text-warning">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_runtime.llm.saved_ineffective)}
            </p>
          )}
          {status.kind === 'error' && (
            <p className="flex items-start gap-1.5 text-[12.5px] text-destructive">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {status.message}
            </p>
          )}
          {(status.kind === 'idle' || status.kind === 'saving') && (
            <p className="text-[12.5px] text-muted-foreground">
              {complete
                ? t(($) => $.step_runtime.llm.skip_note)
                : isCustom
                  ? t(($) => $.step_runtime.llm.incomplete_hint)
                  : t(($) => $.step_runtime.llm.incomplete_hint_key_only)}
            </p>
          )}
        </div>
        <Button type="button" onClick={save} disabled={!complete || saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
          {saving ? t(($) => $.step_runtime.llm.saving) : t(($) => $.step_runtime.llm.save)}
        </Button>
      </div>
    </section>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-[13px]">
        {label}
      </Label>
      {children}
      <p className="text-[11.5px] leading-[1.5] text-muted-foreground">{hint}</p>
    </div>
  );
}
