'use client';

import { useCallback, useEffect, useState } from 'react';
import { Eye, EyeOff, Loader2, Lock, Plus, Save, Trash2 } from 'lucide-react';
import { api } from '@goosar/core/api';
import { useAuthStore } from '@goosar/core/auth';
import type { Agent, AgentEnvResponse } from '@goosar/core/types';
import { AGENT_ENV_MASKED_VALUE } from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { toast } from 'sonner';
import { useT } from '../../../i18n';

let nextEnvId = 0;

interface EnvEntry {
  id: number;
  key: string;
  value: string;
  visible: boolean;
  masked: boolean;
}

function isMaskedResponse(resp: AgentEnvResponse): boolean {
  if (resp.values_masked === true) return true;
  if (resp.values_masked === false) return false;
  const values = Object.values(resp.custom_env ?? {});
  return values.length > 0 && values.every((v) => v === AGENT_ENV_MASKED_VALUE);
}

function envMapToEntries(env: Record<string, string>, masked: boolean): EnvEntry[] {
  return Object.entries(env).map(([key, value]) => {
    const isMasked = masked && value === AGENT_ENV_MASKED_VALUE;
    return {
      id: nextEnvId++,
      key,
      value: isMasked ? '' : value,
      visible: false,
      masked: isMasked,
    };
  });
}

function entriesToEnvMap(entries: EnvEntry[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (!key) continue;
    map[key] = entry.masked && entry.value === '' ? AGENT_ENV_MASKED_VALUE : entry.value;
  }
  return map;
}

export function EnvTab({
  agent,
  onDirtyChange,
  onSaved,
}: {
  agent: Agent;
  onDirtyChange?: (dirty: boolean) => void;
  onSaved?: () => void;
}) {
  const { t } = useT('agents');

  const [revealed, setRevealed] = useState<EnvEntry[] | null>(null);
  const [originalMap, setOriginalMap] = useState<Record<string, string>>({});
  const [revealing, setRevealing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [valuesMasked, setValuesMasked] = useState(false);

  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const isAgentOwner = currentUserId !== null && agent.owner_id === currentUserId;

  const keyCount = agent.custom_env_key_count ?? 0;

  const currentEnvMap = revealed ? entriesToEnvMap(revealed) : originalMap;
  const dirty = revealed !== null && JSON.stringify(currentEnvMap) !== JSON.stringify(originalMap);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleReveal = useCallback(async () => {
    setRevealing(true);
    try {
      const resp = await api.getAgentEnv(agent.id);
      const env = resp.custom_env ?? {};
      const masked = isMaskedResponse(resp);
      setValuesMasked(masked);
      setOriginalMap(env);
      setRevealed(envMapToEntries(env, masked));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.env.reveal_failed_toast),
      );
    } finally {
      setRevealing(false);
    }
  }, [agent.id, t]);

  const addEnvEntry = () => {
    setRevealed((prev) => [
      ...(prev ?? []),
      { id: nextEnvId++, key: '', value: '', visible: true, masked: false },
    ]);
  };

  const removeEnvEntry = (index: number) => {
    setRevealed((prev) => (prev ?? []).filter((_, i) => i !== index));
  };

  const updateEnvEntry = (index: number, field: 'key' | 'value', val: string) => {
    setRevealed((prev) =>
      (prev ?? []).map((entry, i) => (i === index ? { ...entry, [field]: val } : entry)),
    );
  };

  const toggleEnvVisibility = (index: number) => {
    setRevealed((prev) =>
      (prev ?? []).map((entry, i) => (i === index ? { ...entry, visible: !entry.visible } : entry)),
    );
  };

  const handleSave = async () => {
    if (revealed === null) return;
    const keys = revealed.filter((e) => e.key.trim()).map((e) => e.key.trim());
    const uniqueKeys = new Set(keys);
    if (uniqueKeys.size < keys.length) {
      toast.error(t(($) => $.tab_body.env.duplicate_keys_toast));
      return;
    }

    const isUntouchedPlaintextSentinel = (entry: EnvEntry): boolean =>
      !valuesMasked && !entry.masked && originalMap[entry.key.trim()] === AGENT_ENV_MASKED_VALUE;
    if (
      revealed.some((e) => e.value === AGENT_ENV_MASKED_VALUE && !isUntouchedPlaintextSentinel(e))
    ) {
      toast.error(t(($) => $.tab_body.env.sentinel_value_toast));
      return;
    }

    setSaving(true);
    try {
      const resp = await api.updateAgentEnv(agent.id, {
        custom_env: currentEnvMap,
      });
      const env = resp.custom_env ?? {};
      const masked = isMaskedResponse(resp);
      setValuesMasked(masked);
      setOriginalMap(env);
      setRevealed(envMapToEntries(env, masked));
      toast.success(t(($) => $.tab_body.env.saved_toast));
      onSaved?.();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.env.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  if (revealed === null) {
    return (
      <div className="space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <p className="flex items-center gap-2 text-sm font-medium">
              <Lock className="h-3.5 w-3.5 text-muted-foreground" />
              {keyCount > 0
                ? t(($) => $.tab_body.env.not_revealed_title, {
                    count: keyCount,
                  })
                : t(($) => $.tab_body.env.not_revealed_empty)}
            </p>
            <p className="text-xs text-muted-foreground">
              {isAgentOwner
                ? t(($) => $.tab_body.env.not_revealed_hint)
                : t(($) => $.tab_body.env.not_revealed_hint_foreign)}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={revealing}
            onClick={handleReveal}
            className="shrink-0"
          >
            {revealing ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Eye className="h-3.5 w-3.5" />
            )}
            {revealing
              ? t(($) => $.tab_body.env.revealing)
              : isAgentOwner
                ? t(($) => $.tab_body.env.reveal_action)
                : t(($) => $.tab_body.env.manage_action)}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {valuesMasked && (
        <p
          data-testid="env-masked-notice"
          className="flex items-start gap-2 rounded-md border border-border bg-muted/40 p-2.5 text-xs text-muted-foreground"
        >
          <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t(($) => $.tab_body.env.masked_notice)}</span>
        </p>
      )}
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.env.intro_prefix)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            {'ANTHROPIC_API_KEY'}
          </code>
          {t(($) => $.tab_body.env.intro_separator)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            {'ANTHROPIC_BASE_URL'}
          </code>
          {t(($) => $.tab_body.env.intro_suffix)}
        </p>
        {/* GH #273: a masked (non-owner) session may only preserve or
            remove entries — the server 403s any literal value — so the
            add affordance and the value inputs are withheld rather than
            offered as editors whose save can only fail. */}
        {!valuesMasked && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addEnvEntry}
            className="shrink-0"
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.tab_body.common.add)}
          </Button>
        )}
      </div>

      {revealed.length > 0 ? (
        <div className="space-y-2">
          {revealed.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              {/*
                A masked entry's key is locked. Renaming it would send a
                `****` value under a name the server has never stored,
                and the server drops exactly that combination — the old
                key would be removed and the new one discarded, losing
                the owner's secret with no way to get it back. Removing
                the entry and adding a fresh one stays available and is
                honest about what it does.
              */}
              <Input
                value={entry.key}
                onChange={(e) => updateEnvEntry(index, 'key', e.target.value)}
                placeholder={t(($) => $.tab_body.env.key_placeholder)}
                className="w-[40%] font-mono text-xs"
                disabled={entry.masked}
                title={entry.masked ? t(($) => $.tab_body.env.masked_key_locked_title) : undefined}
              />
              <div className="relative flex-1">
                {/*
                  A masked entry renders EMPTY, with a placeholder saying
                  the stored value is hidden. Two reasons, both about not
                  lying: a field pre-filled with `****` looks like a value
                  the user is holding, and typing into it would append to
                  the placeholder and store `****newsecret`. Empty and
                  labelled is the honest shape — and leaving it empty is
                  what preserves the stored value.
                */}
                <Input
                  type={entry.visible ? 'text' : 'password'}
                  value={entry.value}
                  onChange={(e) => updateEnvEntry(index, 'value', e.target.value)}
                  placeholder={
                    entry.masked
                      ? t(($) => $.tab_body.env.masked_value_placeholder)
                      : t(($) => $.tab_body.env.value_placeholder)
                  }
                  className="pr-8 font-mono text-xs"
                  disabled={valuesMasked}
                  aria-label={entry.masked ? t(($) => $.tab_body.env.masked_value_aria) : undefined}
                />
                {/*
                  The reveal toggle shows what the USER typed, so it only
                  appears once there is something of theirs to show. On an
                  untouched masked entry there is nothing behind the field,
                  and an eye icon would suggest otherwise.
                */}
                {(!entry.masked || entry.value !== '') && (
                  <button
                    type="button"
                    onClick={() => toggleEnvVisibility(index)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    aria-label={
                      entry.visible
                        ? t(($) => $.tab_body.env.hide_value_aria)
                        : t(($) => $.tab_body.env.show_value_aria)
                    }
                  >
                    {entry.visible ? (
                      <EyeOff className="h-3.5 w-3.5" />
                    ) : (
                      <Eye className="h-3.5 w-3.5" />
                    )}
                  </button>
                )}
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => removeEnvEntry(index)}
                className="text-muted-foreground hover:text-destructive"
                aria-label={t(($) => $.tab_body.env.remove_aria)}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs italic text-muted-foreground">
          {t(($) => $.tab_body.env.empty_editable)}
        </p>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}
