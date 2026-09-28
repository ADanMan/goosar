'use client';

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@goosar/ui/components/ui/select';
import {
  DEFAULT_SESSION_POLICY,
  REQUIRE_MFA_VALUES,
  readSessionPolicy,
  type RequireMFA,
  type SessionPolicy,
} from '@goosar/core/api';
import type { DeploymentPolicyDoc } from '@goosar/core/api/deployment-admin';
import { deploymentPolicyOptions, useUpdateDeploymentPolicy } from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';
import { SectionNotice, type SectionNoticeState } from './section-notice';

function NumberRow({
  id,
  label,
  hint,
  value,
  min,
  max,
  error,
  onChange,
  onValidChange,
}: {
  id: string;
  label: string;
  hint: string;
  value: number;
  min: number;
  max: number;
  error: string;
  onChange: (next: number) => void;
  onValidChange: (valid: boolean) => void;
}) {
  const [draft, setDraft] = useState(String(value));
  useEffect(() => setDraft(String(value)), [value]);

  const parsed = Number(draft);
  const invalid =
    draft.trim() === '' || !Number.isInteger(parsed) || parsed < min || parsed > max;

  useEffect(() => {
    onValidChange(!invalid);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invalid]);

  return (
    <div className="space-y-1">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        inputMode="numeric"
        aria-invalid={invalid}
        aria-describedby={invalid ? `${id}-error` : undefined}
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value);
          const next = Number(e.target.value);
          if (e.target.value.trim() !== '' && Number.isInteger(next) && next >= min && next <= max) {
            onChange(next);
          }
        }}
      />
      {invalid ? (
        <p id={`${id}-error`} className="text-xs text-destructive">
          {error}
        </p>
      ) : (
        <p className="text-xs text-muted-foreground">{hint}</p>
      )}
    </div>
  );
}

export function DeploymentSessionsSection() {
  const { t } = useT('settings');
  const { data } = useQuery(deploymentPolicyOptions());
  const update = useUpdateDeploymentPolicy();

  const requireMfaOptions = REQUIRE_MFA_VALUES.map((value) => ({
    value,
    label: t(($) => $.deployment_sessions.require_mfa_options[value]),
  }));

  const stored = readSessionPolicy(data?.policy ?? {});
  const [draft, setDraft] = useState<SessionPolicy>(stored);
  const storedKey = JSON.stringify(stored);
  useEffect(() => {
    setDraft(JSON.parse(storedKey) as SessionPolicy);
  }, [storedKey]);

  const [notice, setNotice] = useState<SectionNoticeState | null>(null);
  const [idleValid, setIdleValid] = useState(true);
  const [absoluteValid, setAbsoluteValid] = useState(true);
  const [maxValid, setMaxValid] = useState(true);
  const allValid = idleValid && absoluteValid && maxValid;

  const dirty = JSON.stringify(draft) !== storedKey;

  const save = () => {
    if (!allValid) return;
    const next: DeploymentPolicyDoc = { ...(data?.policy ?? {}), session: { ...draft } };
    update.mutate(next, {
      onSuccess: () => setNotice({ tone: 'success', message: t(($) => $.deployment_sessions.saved) }),
      onError: (err: unknown) =>
        setNotice({
          tone: 'destructive',
          message: err instanceof Error ? err.message : t(($) => $.deployment_sessions.save_failed),
        }),
    });
  };

  return (
    <SettingsSection
      title={t(($) => $.deployment_sessions.title)}
      description={t(($) => $.deployment_sessions.description)}
    >
      <SettingsCard>
        <div className="space-y-4 p-4">
          <NumberRow
            id="session-idle"
            label={t(($) => $.deployment_sessions.idle_label)}
            hint={t(($) => $.deployment_sessions.idle_hint)}
            value={draft.idle_timeout_hours}
            min={1}
            max={720}
            error={t(($) => $.deployment_sessions.idle_error)}
            onChange={(idle_timeout_hours) => setDraft((prev) => ({ ...prev, idle_timeout_hours }))}
            onValidChange={setIdleValid}
          />
          <NumberRow
            id="session-absolute"
            label={t(($) => $.deployment_sessions.absolute_label)}
            hint={t(($) => $.deployment_sessions.absolute_hint)}
            value={draft.absolute_lifetime_days}
            min={1}
            max={365}
            error={t(($) => $.deployment_sessions.absolute_error)}
            onChange={(absolute_lifetime_days) =>
              setDraft((prev) => ({ ...prev, absolute_lifetime_days }))
            }
            onValidChange={setAbsoluteValid}
          />
          <NumberRow
            id="session-max"
            label={t(($) => $.deployment_sessions.max_label)}
            hint={t(($) => $.deployment_sessions.max_hint)}
            value={draft.max_concurrent_sessions}
            min={0}
            max={1000}
            error={t(($) => $.deployment_sessions.max_error)}
            onChange={(max_concurrent_sessions) =>
              setDraft((prev) => ({ ...prev, max_concurrent_sessions }))
            }
            onValidChange={setMaxValid}
          />
          <div className="space-y-1">
            <Label htmlFor="session-require-mfa">
              {t(($) => $.deployment_sessions.require_mfa_label)}
            </Label>
            <Select
              items={requireMfaOptions}
              value={draft.require_mfa}
              onValueChange={(value) => {
                if (!value) return;
                setDraft((prev) => ({ ...prev, require_mfa: value as RequireMFA }));
              }}
            >
              <SelectTrigger id="session-require-mfa" className="w-full">
                <SelectValue>
                  {requireMfaOptions.find((option) => option.value === draft.require_mfa)?.label}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {requireMfaOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.deployment_sessions.require_mfa_hint)}
            </p>
          </div>
          <div className="flex gap-2">
            <Button disabled={!dirty || !allValid || update.isPending} onClick={save}>
              {t(($) => $.deployment_sessions.save)}
            </Button>
            <Button
              variant="ghost"
              disabled={!dirty}
              onClick={() => {
                setDraft(JSON.parse(storedKey) as SessionPolicy);
                setNotice(null);
              }}
            >
              {t(($) => $.deployment_sessions.reset)}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.deployment_sessions.defaults_note, {
              idle: DEFAULT_SESSION_POLICY.idle_timeout_hours,
              days: DEFAULT_SESSION_POLICY.absolute_lifetime_days,
            })}
          </p>
        </div>
      </SettingsCard>

      <SectionNotice
        notice={notice}
        onDismiss={() => setNotice(null)}
        dismissLabel={t(($) => $.deployment_sessions.notice_dismiss)}
      />
    </SettingsSection>
  );
}
