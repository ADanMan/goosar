import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { AlertCircle, AlertTriangle, CheckCircle2, Info, LogIn } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { Switch } from '@goosar/ui/components/ui/switch';
import { cn } from '@goosar/ui/lib/utils';
import { toast } from 'sonner';
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from '@goosar/views/settings';
import { KerberosSignInPanel } from './kerberos-settings-panel';
import { useT } from '@goosar/views/i18n';
import { reauthenticateDaemon } from '../platform/daemon-reauth';
import type { LlmAuthReachability } from '../../../shared/perimeter-config';
import type {
  LlmRuntimeConfigView,
  LlmRuntimeSaveResult,
} from '../../../shared/llm-runtime-settings';
import { NetworkStatusPanel, useNetworkStatus } from './network-status';
import { UninstallSection } from './uninstall-section';
import type { DaemonPrefs, DaemonStatus } from '../../../shared/daemon-types';
import {
  AGENT_CONFIG_FIELDS,
  AGENT_RUNTIME_COLORS,
  type AgentConfigField,
  type AgentConfigPatch,
  type AgentConfigSaveResult,
  type AgentRunnerState,
  type AgentRuntimeStatus,
} from '../../../shared/agent-runtime-types';
import {
  agentConfigFieldHint,
  agentConfigFieldLabel,
  agentRuntimeDescription,
  agentRuntimeLabel,
  daemonStateLabel,
  listFieldLabels,
} from './agent-runtime-copy';
import { DAEMON_STATE_COLORS, formatUptime } from '../../../shared/daemon-types';

function DiagnosticsRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="grid grid-cols-[140px_minmax(0,1fr)] items-baseline gap-3 py-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={cn('min-w-0 truncate text-sm', mono && 'font-mono text-xs')}
        title={typeof value === 'string' ? value : undefined}
      >
        {value}
      </span>
    </div>
  );
}

const AGENT_FIELD_PLACEHOLDERS: Record<AgentConfigField, string> = {
  'llm.api_base': 'https://gateway.example.corp/v1',
  'llm.model': 'openai/glm-4.6',
  'llm.api_key': '',
};

type AgentSaveOutcome =
  | {
      kind: 'answered';
      attempted: AgentConfigField[];
      result: AgentConfigSaveResult;
    }
  | { kind: 'unanswered'; attempted: AgentConfigField[]; message: string };

function fieldsAppliedBefore(
  attempted: AgentConfigField[],
  failed: AgentConfigField | null,
): AgentConfigField[] {
  if (failed === null) return [];
  const stoppedAt = AGENT_CONFIG_FIELDS.indexOf(failed);
  return attempted.filter((field) => AGENT_CONFIG_FIELDS.indexOf(field) < stoppedAt);
}

function AgentRuntimeSetup({
  status,
  onSaved,
  onOutcome,
}: {
  status: Extract<AgentRuntimeStatus, { state: 'needs_config' }>;
  onSaved: () => Promise<void>;
  onOutcome: (outcome: AgentSaveOutcome | null) => void;
}) {
  const { t } = useT('settings');
  const [draft, setDraft] = useState<AgentConfigPatch>({});
  const [saving, setSaving] = useState(false);

  const forgetSecret = useCallback(() => {
    setDraft((current) => ({ ...current, 'llm.api_key': '' }));
  }, []);

  const patch = useMemo(() => {
    const next: AgentConfigPatch = {};
    for (const field of AGENT_CONFIG_FIELDS) {
      const value = draft[field]?.trim();
      if (value) next[field] = value;
    }
    return next;
  }, [draft]);
  const hasChanges = Object.keys(patch).length > 0;

  const save = useCallback(async () => {
    setSaving(true);
    onOutcome(null);
    const attempted = AGENT_CONFIG_FIELDS.filter((field) => patch[field] !== undefined);
    try {
      const result = await window.daemonAPI.setAgentRuntimeConfig(patch);
      forgetSecret();
      onOutcome({ kind: 'answered', attempted, result });
      if (result.ok) {
        toast.success(t(($) => $.desktop.agent_setup.saved_toast));
        await onSaved();
      }
    } catch (err) {
      forgetSecret();
      onOutcome({
        kind: 'unanswered',
        attempted,
        message: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }, [patch, forgetSecret, onSaved, onOutcome, t]);

  return (
    <SettingsSection
      title={t(($) => $.desktop.agent_setup.title)}
      description={
        status.configPath === null
          ? t(($) => $.desktop.agent_setup.description_no_config)
          : t(($) => $.desktop.agent_setup.description)
      }
    >
      <SettingsCard>
        {status.configPath !== null && (
          <>
            {AGENT_CONFIG_FIELDS.map((field) => (
              <SettingsRow
                key={field}
                size="text"
                align="start"
                label={
                  <span className="flex items-center gap-2">
                    <Label htmlFor={`agent-config-${field}`}>
                      {agentConfigFieldLabel(t, field)}
                    </Label>
                    {status.missing.includes(field) && (
                      <span className="text-xs font-normal text-warning">
                        {t(($) => $.desktop.agent_setup.needed_badge)}
                      </span>
                    )}
                  </span>
                }
                description={agentConfigFieldHint(t, field)}
              >
                <Input
                  id={`agent-config-${field}`}
                  type={field === 'llm.api_key' ? 'password' : 'text'}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={AGENT_FIELD_PLACEHOLDERS[field]}
                  value={draft[field] ?? ''}
                  disabled={saving}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      [field]: event.target.value,
                    }))
                  }
                />
              </SettingsRow>
            ))}

            <SettingsRow
              label={t(($) => $.desktop.agent_setup.apply_label)}
              description={t(($) => $.desktop.agent_setup.apply_description)}
            >
              <Button onClick={save} disabled={saving || !hasChanges}>
                {saving
                  ? t(($) => $.desktop.agent_setup.saving)
                  : t(($) => $.desktop.agent_setup.save_button)}
              </Button>
            </SettingsRow>
          </>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}

function OutcomeBanner({
  tone,
  icon,
  title,
  children,
}: {
  tone: 'error' | 'warning' | 'neutral';
  icon: ReactNode;
  title?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        'flex items-start gap-3 rounded-lg border px-4 py-3',
        tone === 'error' && 'border-destructive/40 bg-destructive/5',
        tone === 'warning' && 'border-warning/40 bg-warning/5',
        tone === 'neutral' && 'bg-muted/30',
      )}
    >
      {icon}
      <div className="min-w-0 flex-1">
        {title !== undefined && (
          <p className={cn('text-sm font-medium', tone === 'error' && 'text-destructive')}>
            {title}
          </p>
        )}
        {children}
      </div>
    </div>
  );
}

function RetypeKeyNote({ attempted }: { attempted: AgentConfigField[] }) {
  const { t } = useT('settings');
  if (!attempted.includes('llm.api_key')) return null;
  return (
    <p className="mt-1 text-xs text-muted-foreground">
      {t(($) => $.desktop.agent_setup.retype_key_note)}
    </p>
  );
}

function AgentSetupOutcome({ outcome }: { outcome: AgentSaveOutcome }) {
  const { t } = useT('settings');
  if (outcome.kind === 'unanswered') {
    return (
      <OutcomeBanner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title={t(($) => $.desktop.agent_outcome.unanswered_title)}
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">{outcome.message}</p>
        <p className="mt-1 text-sm text-muted-foreground">
          {t(($) => $.desktop.agent_outcome.unanswered_note)}
        </p>
        <RetypeKeyNote attempted={outcome.attempted} />
      </OutcomeBanner>
    );
  }

  const result = outcome.result;

  if (!result.ok) {
    const applied = fieldsAppliedBefore(outcome.attempted, result.field);
    return (
      <OutcomeBanner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title={
          applied.length === 0
            ? t(($) => $.desktop.agent_outcome.nothing_changed_title)
            : t(($) => $.desktop.agent_outcome.partial_title)
        }
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">{result.message}</p>
        {applied.length > 0 && (
          <p className="mt-1 text-sm text-muted-foreground">
            {t(($) => $.desktop.agent_outcome.applied_note, {
              fields: listFieldLabels(t, applied),
              count: applied.length,
            })}
          </p>
        )}
        <RetypeKeyNote attempted={outcome.attempted} />
      </OutcomeBanner>
    );
  }

  if (result.shadowed.length > 0) {
    return (
      <OutcomeBanner
        tone="warning"
        icon={<AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
        title={t(($) => $.desktop.agent_outcome.shadowed_title)}
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">
          {t(($) => $.desktop.agent_outcome.shadowed_note, {
            fields: listFieldLabels(t, result.shadowed),
            keys: result.shadowed.join(', '),
            path: result.path,
          })}
        </p>
      </OutcomeBanner>
    );
  }

  return (
    <OutcomeBanner
      tone="neutral"
      icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
    >
      <p className="min-w-0 break-words text-sm text-muted-foreground">
        {t(($) => $.desktop.agent_outcome.saved_note, { path: result.path })}
      </p>
    </OutcomeBanner>
  );
}

function NetworkSection() {
  const { t } = useT('settings');
  const { state, busy, recheck } = useNetworkStatus();

  if (state === null) return null;

  return (
    <SettingsSection
      title={t(($) => $.desktop.perimeter.title)}
      description={t(($) => $.desktop.perimeter.description)}
    >
      <SettingsCard>
        <NetworkStatusPanel state={state} busy={busy} onRecheck={() => void recheck()} />
      </SettingsCard>
    </SettingsSection>
  );
}

function KerberosSection() {
  const { t } = useT('settings');
  const { state } = useNetworkStatus({ subscribe: false });

  if (state === null || state.kerberosSupported !== true) return null;

  return (
    <SettingsSection
      title={t(($) => $.desktop.perimeter.kerberos_title)}
      description={t(($) => $.desktop.perimeter.kerberos_description)}
    >
      <SettingsCard>
        <KerberosSignInPanel />
      </SettingsCard>
    </SettingsSection>
  );
}

function ReachabilityBanner({ fact }: { fact: LlmAuthReachability | null }) {
  const { t } = useT('settings');
  if (fact === null) return null;
  switch (fact.kind) {
    case 'response':
      if (fact.status === 401 || fact.status === 403) {
        return (
          <OutcomeBanner
            tone="error"
            icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
            title={t(($) => $.desktop.llm.reach_auth_rejected_title)}
          >
            <p className="mt-0.5 text-sm text-muted-foreground">
              {t(($) => $.desktop.llm.reach_auth_rejected_note, { status: fact.status })}
            </p>
          </OutcomeBanner>
        );
      }
      return fact.status >= 500 ? (
        <OutcomeBanner
          tone="warning"
          icon={<AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
          title={t(($) => $.desktop.llm.reach_erroring_title)}
        >
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.reach_erroring_note, { status: fact.status })}
          </p>
        </OutcomeBanner>
      ) : (
        <OutcomeBanner
          tone="neutral"
          icon={<CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-500" />}
          title={t(($) => $.desktop.llm.reach_ok_title)}
        >
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.reach_ok_note, { status: fact.status })}
          </p>
        </OutcomeBanner>
      );
    case 'no_key':
      return (
        <OutcomeBanner
          tone="neutral"
          icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
          title={t(($) => $.desktop.llm.reach_no_key_title)}
        >
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.reach_no_key_note)}
          </p>
        </OutcomeBanner>
      );
    case 'unreachable':
      return (
        <OutcomeBanner
          tone="error"
          icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
          title={t(($) => $.desktop.llm.reach_unreachable_title)}
        >
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.reach_unreachable_note)}
          </p>
        </OutcomeBanner>
      );
    case 'unconfigured':
    case 'skipped':
      return (
        <OutcomeBanner
          tone="neutral"
          icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
        >
          <p className="min-w-0 text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.reach_nothing_note)}
          </p>
        </OutcomeBanner>
      );
  }
}

function LlmSaveOutcome({ outcome }: { outcome: LlmRuntimeSaveResult }) {
  const { t } = useT('settings');
  if (outcome.status === 'rejected') {
    return (
      <OutcomeBanner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title={t(($) => $.desktop.llm.not_saved_title)}
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">
          {outcome.rejection.message}
        </p>
      </OutcomeBanner>
    );
  }

  const { saved, reachability } = outcome;
  if (!saved.ok) {
    return (
      <OutcomeBanner
        tone="error"
        icon={<AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />}
        title={t(($) => $.desktop.llm.runtime_rejected_title)}
      >
        <p className="mt-0.5 break-words text-sm text-muted-foreground">{saved.message}</p>
      </OutcomeBanner>
    );
  }

  return (
    <div className="space-y-3">
      {saved.shadowed.length > 0 ? (
        <OutcomeBanner
          tone="warning"
          icon={<AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
          title={t(($) => $.desktop.llm.shadowed_title)}
        >
          <p className="mt-0.5 break-words text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.shadowed_note, {
              fields: listFieldLabels(t, saved.shadowed),
              keys: saved.shadowed.join(', '),
              path: saved.path,
            })}
          </p>
        </OutcomeBanner>
      ) : (
        <OutcomeBanner
          tone="neutral"
          icon={<Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
        >
          <p className="min-w-0 break-words text-sm text-muted-foreground">
            {t(($) => $.desktop.llm.saved_note, { path: saved.path })}
          </p>
        </OutcomeBanner>
      )}
      <ReachabilityBanner fact={reachability} />
    </div>
  );
}

function LlmModelSection() {
  const { t } = useT('settings');
  const [config, setConfig] = useState<LlmRuntimeConfigView | null>(null);
  const [draft, setDraft] = useState({ apiBase: '', model: '' });
  const [saving, setSaving] = useState(false);
  const [outcome, setOutcome] = useState<LlmRuntimeSaveResult | null>(null);

  const seedFrom = useCallback((view: LlmRuntimeConfigView) => {
    setConfig(view);
    setDraft({ apiBase: view.apiBase ?? '', model: view.model ?? '' });
  }, []);

  const refresh = useCallback(async () => {
    try {
      const view = await window.daemonAPI?.getLlmRuntimeConfig?.();
      if (view) seedFrom(view);
      else setConfig(null);
    } catch {
      setConfig(null);
    }
  }, [seedFrom]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const save = useCallback(async () => {
    setSaving(true);
    setOutcome(null);
    try {
      const result = await window.daemonAPI.saveLlmRuntimeConfig({
        apiBase: draft.apiBase.trim(),
        model: draft.model.trim(),
      });
      setOutcome(result);
      if (result.status === 'rejected') {
        toast.error(result.rejection.message);
      } else if (result.saved.ok) {
        toast.success(t(($) => $.desktop.llm.saved_toast));
        await refresh();
      } else {
        toast.error(result.saved.message);
      }
    } catch (err) {
      setOutcome(null);
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, [draft, refresh, t]);

  if (config === null) return null;

  const canSave = !saving && draft.apiBase.trim().length > 0 && draft.model.trim().length > 0;

  return (
    <SettingsSection
      title={t(($) => $.desktop.llm.title)}
      description={t(($) => $.desktop.llm.description)}
    >
      <SettingsCard>
        <SettingsRow
          size="text"
          align="start"
          label={<Label htmlFor="llm-runtime-model">{t(($) => $.desktop.llm.model_label)}</Label>}
          description={t(($) => $.desktop.llm.model_description)}
        >
          <Input
            id="llm-runtime-model"
            value={draft.model}
            autoComplete="off"
            spellCheck={false}
            placeholder="openai/glm-4.6"
            disabled={saving}
            onChange={(e) => setDraft((current) => ({ ...current, model: e.target.value }))}
          />
        </SettingsRow>

        <SettingsRow
          size="text"
          align="start"
          label={<Label htmlFor="llm-runtime-base">{t(($) => $.desktop.llm.gateway_label)}</Label>}
          description={t(($) => $.desktop.llm.gateway_description)}
        >
          <Input
            id="llm-runtime-base"
            value={draft.apiBase}
            autoComplete="off"
            spellCheck={false}
            placeholder="https://gateway.example/v1"
            disabled={saving}
            onChange={(e) => setDraft((current) => ({ ...current, apiBase: e.target.value }))}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.llm.apply_label)}
          description={
            config.hasKey
              ? t(($) => $.desktop.llm.apply_key_kept)
              : t(($) => $.desktop.llm.apply_no_key)
          }
        >
          <Button onClick={save} disabled={!canSave}>
            {saving ? t(($) => $.desktop.llm.saving) : t(($) => $.desktop.llm.save_button)}
          </Button>
        </SettingsRow>
      </SettingsCard>

      {outcome !== null && (
        <div className="mt-4">
          <LlmSaveOutcome outcome={outcome} />
        </div>
      )}
    </SettingsSection>
  );
}

export function DaemonSettingsTab() {
  const { t } = useT('settings');
  const [prefs, setPrefs] = useState<DaemonPrefs>({ autoStart: true, autoStop: false });
  const [cliInstalled, setCliInstalled] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<DaemonStatus>({ state: 'stopped' });
  const [agentRuntime, setAgentRuntime] = useState<AgentRuntimeStatus>({
    state: 'checking',
  });
  const [agentSave, setAgentSave] = useState<AgentSaveOutcome | null>(null);
  const [reauthLoading, setReauthLoading] = useState(false);
  const [agentRunner, setAgentRunner] = useState<AgentRunnerState | null>(null);
  const [runnerSaving, setRunnerSaving] = useState(false);

  const refreshAgentRuntime = useCallback(
    () => window.daemonAPI.getAgentRuntime().then(setAgentRuntime),
    [],
  );

  useEffect(() => {
    window.daemonAPI.getPrefs().then(setPrefs);
    window.daemonAPI.isCliInstalled().then(setCliInstalled);
    window.daemonAPI.getStatus().then(setStatus);
    void refreshAgentRuntime();
    window.daemonAPI.getAgentRunner().then(setAgentRunner);
    return window.daemonAPI.onStatusChange(setStatus);
  }, [refreshAgentRuntime]);

  const handleReauth = useCallback(async () => {
    setReauthLoading(true);
    await reauthenticateDaemon();
    setReauthLoading(false);
  }, []);

  const updateRunner = useCallback(
    async (useBundled: boolean) => {
      setRunnerSaving(true);
      try {
        const result = await window.daemonAPI.setAgentRunner(useBundled ? 'hermes' : 'none');
        if (!result.success) throw new Error(result.error);
        setAgentRunner(await window.daemonAPI.getAgentRunner());
        setAgentRuntime(result.runtime);
        toast.success(t(($) => $.desktop.daemon.agent_runner_saved_toast));
      } catch (error) {
        toast.error(
          t(($) => $.desktop.daemon.agent_runner_failed_toast),
          {
            description: error instanceof Error ? error.message : undefined,
          },
        );
      } finally {
        setRunnerSaving(false);
      }
    },
    [t],
  );

  const updatePref = useCallback(
    async (key: keyof DaemonPrefs, value: boolean) => {
      setSaving(true);
      try {
        const updated = await window.daemonAPI.setPrefs({ [key]: value });
        setPrefs(updated);
        toast.success(
          t(($) => $.desktop.daemon.prefs_saved_toast),
          {
            id: 'settings-auto-save',
          },
        );
      } catch (error) {
        toast.error(
          t(($) => $.desktop.daemon.prefs_save_failed_toast),
          {
            description: error instanceof Error ? error.message : undefined,
          },
        );
      } finally {
        setSaving(false);
      }
    },
    [t],
  );

  const externallyManaged = status.externallyManaged === true;

  return (
    <SettingsTab
      title={t(($) => $.desktop.daemon.title)}
      description={t(($) => $.desktop.daemon.description)}
    >
      {status.state === 'auth_expired' && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-destructive">
              {t(($) => $.desktop.daemon.auth_expired_title)}
            </p>
            <p className="mt-0.5 text-sm text-muted-foreground">
              {t(($) => $.desktop.daemon.auth_expired_body)}
            </p>
          </div>
          <Button size="sm" className="shrink-0" onClick={handleReauth} disabled={reauthLoading}>
            <LogIn className="size-3.5 mr-1.5" />
            {t(($) => $.desktop.daemon.auth_expired_action)}
          </Button>
        </div>
      )}

      {externallyManaged && (
        <div className="mt-4 flex items-start gap-3 rounded-lg border bg-muted/30 px-4 py-3">
          <Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
          <p className="min-w-0 text-sm text-muted-foreground">
            {/* The commands stay verbatim: they are what the user types. */}
            {t(($) => $.desktop.daemon.externally_managed, {
              start: 'goosar daemon start',
              stop: 'goosar daemon stop',
            })}
          </p>
        </div>
      )}

      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.daemon.auto_start_label)}
          description={t(($) => $.desktop.daemon.auto_start_description)}
        >
          <Switch
            checked={prefs.autoStart}
            onCheckedChange={(checked) => updatePref('autoStart', checked)}
            disabled={saving || externallyManaged}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.daemon.auto_stop_label)}
          description={t(($) => $.desktop.daemon.auto_stop_description)}
        >
          <Switch
            checked={prefs.autoStop}
            onCheckedChange={(checked) => updatePref('autoStop', checked)}
            disabled={saving || externallyManaged}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.daemon.agent_runner_label)}
          description={
            agentRunner !== null && !agentRunner.bundledAvailable
              ? t(($) => $.desktop.daemon.agent_runner_unavailable)
              : t(($) => $.desktop.daemon.agent_runner_description)
          }
        >
          <Switch
            aria-label={t(($) => $.desktop.daemon.agent_runner_switch)}
            checked={agentRunner?.choice === 'hermes'}
            onCheckedChange={(checked) => updateRunner(checked)}
            disabled={agentRunner === null || !agentRunner.bundledAvailable || runnerSaving}
          />
        </SettingsRow>

        {/* The agent runtime is a separate payload from the goosar CLI: the
            CLI drives the daemon, hermes is what actually runs a task. They
            fail independently, so they get their own rows. */}
        <SettingsRow
          label={t(($) => $.desktop.daemon.agent_runtime_label)}
          description={agentRuntimeDescription(t, agentRuntime)}
          align="start"
        >
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
            <span
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                AGENT_RUNTIME_COLORS[agentRuntime.state],
              )}
            />
            {agentRuntimeLabel(t, agentRuntime.state)}
          </span>
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.daemon.cli_status_label)}
          description={
            cliInstalled === null
              ? t(($) => $.desktop.daemon.cli_status_checking)
              : cliInstalled
                ? t(($) => $.desktop.daemon.cli_status_installed)
                : t(($) => $.desktop.daemon.cli_status_missing)
          }
        >
          {cliInstalled === false && (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                window.desktopAPI.openExternal('https://github.com/adanman/goosar#cli-installation')
              }
            >
              {t(($) => $.desktop.daemon.cli_install_guide)}
            </Button>
          )}
          {cliInstalled !== false && <span />}
        </SettingsRow>
      </SettingsCard>

      {/* Change the active LLM model/endpoint (issue #160) once the runtime is
          configured, so a broken or switched model is fixed in-app instead of
          hand-editing config.user.yaml. Only shown when the runtime is ready —
          first-run setup below owns the needs_config case (and the API key). */}
      {agentRuntime.state === 'ready' && <LlmModelSection />}

      {/* Machine-level perimeter transport (issue #46) — its own section
          because it reconfigures every network stack on this computer, not a
          daemon preference. */}
      <NetworkSection />
      <KerberosSection />

      {/* The «Контур» LLM connection + Kerberos realm (issue #115): the
          corporate AI profile the toggle applies, edited alongside the
          transport switch it rides with. */}

      {agentRuntime.state === 'needs_config' && (
        <AgentRuntimeSetup
          status={agentRuntime}
          onSaved={refreshAgentRuntime}
          onOutcome={setAgentSave}
        />
      )}

      {agentSave !== null && (
        <div className="mt-4">
          <AgentSetupOutcome outcome={agentSave} />
        </div>
      )}

      {/* Diagnostics — moved out of the logs panel so the panel can focus
          on logs. These fields matter for support tickets and bug reports,
          not for everyday use. */}
      <SettingsSection
        title={t(($) => $.desktop.daemon.diagnostics_title)}
        description={t(($) => $.desktop.daemon.diagnostics_description)}
      >
        <SettingsCard>
          <div className="px-4 py-2">
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_state)}
              value={
                <span className="inline-flex items-center gap-1.5">
                  <span
                    className={cn('size-1.5 rounded-full', DAEMON_STATE_COLORS[status.state])}
                  />
                  {daemonStateLabel(t, status.state)}
                </span>
              }
            />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_uptime)}
              value={status.uptime ? formatUptime(status.uptime) : '—'}
            />
            <DiagnosticsRow label="PID" value={status.pid ?? '—'} mono={!!status.pid} />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_daemon_id)}
              value={status.daemonId ?? '—'}
              mono={!!status.daemonId}
            />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_profile)}
              value={status.profile || 'default'}
            />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_server_url)}
              value={status.serverUrl ?? '—'}
              mono={!!status.serverUrl}
            />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_device_name)}
              value={status.deviceName ?? '—'}
            />
            <DiagnosticsRow
              label={t(($) => $.desktop.daemon.diagnostics_workspaces)}
              value={typeof status.workspaceCount === 'number' ? status.workspaceCount : '—'}
            />
          </div>
        </SettingsCard>
      </SettingsSection>

      {/* Last, and separated: this is the only surface that removes what the
          product installed, and it must not sit next to a toggle. Its own
          component because the flow — enumerate, opt in, remove, report — is
          bigger than every setting above it put together. */}
      <UninstallSection />
    </SettingsTab>
  );
}
