'use client';

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { ArrowLeft, ArrowRight, Check, Eye, EyeOff, Loader2, TriangleAlert } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@goosar/core/api';
import { isPerimeterDeliveryProfile, useConfigStore } from '@goosar/core/config';
import { useCurrentMember } from '@goosar/core/permissions';
import type { Agent, AgentRuntime } from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { Switch } from '@goosar/ui/components/ui/switch';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { cn } from '@goosar/ui/lib/utils';
import { DragStrip } from '@goosar/views/platform';
import { prepareWorkspaceHelper } from '../../workspace/helper-setup';
import { StepHeader } from '../components/step-header';
import {
  buildWorkToolsMcpConfig,
  completedWorkToolsPresets,
  emptyWorkToolsCredentials,
  isWebhookInputAcceptable,
  isPresetInstalled,
  isWorkToolsPresetComplete,
  WorkToolsCard,
  WORK_TOOLS_CARD_ORDER,
  type WorkToolsCredentials,
} from '../presets';
import type { HelperMcpPresetName } from '../presets';
import { useT } from '../../i18n';

type Status =
  | { kind: 'idle' }
  | { kind: 'saving' }
  | { kind: 'saved'; enabled: HelperMcpPresetName[] }
  | { kind: 'error'; message: string };

export function StepWorkTools({
  wsId,
  runtime,
  onFinish,
  onBack,
  perimeterCaMissing,
  installedMcpNames,
}: {
  wsId: string;
  runtime: AgentRuntime | null;
  onFinish: () => void | Promise<void>;
  onBack?: () => void;
  perimeterCaMissing?: boolean;
  installedMcpNames?: string[];
}) {
  const { t } = useT('onboarding');
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const [agent, setAgent] = useState<Agent | null>(null);
  const [agentError, setAgentError] = useState(false);
  const [credentials, setCredentials] = useState<WorkToolsCredentials>(emptyWorkToolsCredentials);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });
  const [visibleSecrets, setVisibleSecrets] = useState<ReadonlySet<string>>(() => new Set());

  const perimeterProfile = useConfigStore(isPerimeterDeliveryProfile);
  const mailDomain = useConfigStore((s) => s.deploymentHosts.mailDomain);
  const { member: currentMember, isLoading: memberLoading } = useCurrentMember(wsId);
  const perimeterGatePending = perimeterProfile && memberLoading;
  const perimeterGateDenied =
    perimeterProfile && !memberLoading && currentMember?.perimeter_access !== true;

  const visibleCardOrder =
    installedMcpNames === undefined
      ? WORK_TOOLS_CARD_ORDER
      : WORK_TOOLS_CARD_ORDER.filter((name) => isPresetInstalled(name, installedMcpNames));
  const noInstalledMcp = installedMcpNames !== undefined && visibleCardOrder.length === 0;

  useEffect(() => {
    if (perimeterGateDenied || noInstalledMcp) void onFinish();
  }, [perimeterGateDenied, noInstalledMcp, onFinish]);

  useEffect(() => {
    if (perimeterGatePending || perimeterGateDenied || noInstalledMcp) return;
    let cancelled = false;
    void (async () => {
      const found = await prepareWorkspaceHelper(wsId, runtime?.id ?? null);
      if (cancelled) return;
      if (!found) {
        setAgentError(true);
        return;
      }
      setAgent(found);
      setAgentError(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [wsId, runtime?.id ?? null, perimeterGatePending, perimeterGateDenied, noInstalledMcp]);

  const saving = status.kind === 'saving';
  const saved = status.kind === 'saved';
  const redacted = agent?.mcp_config_redacted === true;
  const completed = completedWorkToolsPresets(credentials);
  const webhookInvalid = !isWebhookInputAcceptable(credentials.bitrix24.webhook);
  const canSave = completed.length > 0 && !redacted;
  const inputsDisabled = saving || redacted;

  const patchCredentials = useCallback(
    (patch: (prev: WorkToolsCredentials) => WorkToolsCredentials) => {
      setCredentials(patch);
      setStatus((s) => (s.kind === 'saved' ? { kind: 'idle' } : s));
    },
    [],
  );

  const toggleSecret = useCallback((fieldId: string) => {
    setVisibleSecrets((prev) => {
      const next = new Set(prev);
      if (next.has(fieldId)) next.delete(fieldId);
      else next.add(fieldId);
      return next;
    });
  }, []);

  const save = useCallback(async () => {
    if (saving || !canSave) return;
    setStatus({ kind: 'saving' });
    try {
      const helper = agent ?? (await prepareWorkspaceHelper(wsId, runtime?.id ?? null));
      if (!helper) {
        setStatus({
          kind: 'error',
          message: t(($) => $.step_work_tools.error_generic),
        });
        return;
      }
      if (!agent) {
        setAgent(helper);
        setAgentError(false);
      }
      if (helper.mcp_config_redacted === true) {
        setStatus({
          kind: 'error',
          message: t(($) => $.step_work_tools.redacted_note),
        });
        return;
      }
      const enabled = completedWorkToolsPresets(credentials);
      const document = buildWorkToolsMcpConfig(credentials, helper.mcp_config);
      await api.updateAgent(helper.id, { mcp_config: document });
      setAgent({ ...helper, mcp_config: document });
      setVisibleSecrets(new Set());
      setStatus({ kind: 'saved', enabled });
    } catch (err) {
      const message = t(($) => $.step_work_tools.error_generic);
      setStatus({ kind: 'error', message });
      toast.error(message);
      void err;
    }
  }, [agent, canSave, credentials, runtime, saving, t, wsId]);

  const cardBadge = (name: HelperMcpPresetName): ReactNode => {
    if (status.kind === 'saved' && status.enabled.includes(name)) {
      return (
        <span className="flex items-center gap-1 rounded-full bg-success/10 px-2 py-0.5 text-[11px] font-medium text-success">
          <Check className="h-3 w-3" aria-hidden />
          {t(($) => $.step_work_tools.badge_saved_enabled)}
        </span>
      );
    }
    const complete = isWorkToolsPresetComplete(name, credentials);
    const partial = !complete && presetHasInput(name, credentials);
    return (
      <span
        className={cn(
          'rounded-full px-2 py-0.5 text-[11px] font-medium',
          complete ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground',
        )}
      >
        {complete
          ? t(($) => $.step_work_tools.badge_ready)
          : partial
            ? t(($) => $.step_work_tools.badge_incomplete)
            : t(($) => $.step_work_tools.badge_disabled)}
      </span>
    );
  };

  const secretField = (
    fieldId: string,
    label: string,
    placeholder: string,
    value: string,
    onChange: (value: string) => void,
  ) => {
    const visible = visibleSecrets.has(fieldId);
    return (
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={fieldId} className="text-[13px]">
          {label}
        </Label>
        <div className="relative">
          <Input
            id={fieldId}
            type={visible ? 'text' : 'password'}
            autoComplete="off"
            spellCheck={false}
            disabled={inputsDisabled}
            placeholder={placeholder}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            className="pr-10"
          />
          <button
            type="button"
            onClick={() => toggleSecret(fieldId)}
            aria-label={
              visible
                ? t(($) => $.step_work_tools.hide_secret)
                : t(($) => $.step_work_tools.show_secret)
            }
            aria-pressed={visible}
            className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
          >
            {visible ? (
              <EyeOff className="h-3.5 w-3.5" aria-hidden />
            ) : (
              <Eye className="h-3.5 w-3.5" aria-hidden />
            )}
          </button>
        </div>
      </div>
    );
  };

  const footerHint = saved
    ? t(($) => $.step_work_tools.hint_saved)
    : completed.length > 0
      ? t(($) => $.step_work_tools.hint_ready)
      : t(($) => $.step_work_tools.hint_empty);

  if (perimeterGatePending || perimeterGateDenied || noInstalledMcp) return null;

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
          <StepHeader currentStep="work_tools" />
        </div>
      </header>

      <main ref={mainRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[760px] px-6 py-10 sm:px-10 md:px-14 lg:py-14">
          <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {t(($) => $.step_work_tools.eyebrow)}
            </span>
            <span className="rounded-full border px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
              {t(($) => $.step_work_tools.optional_badge)}
            </span>
          </div>
          <h1 className="text-balance font-serif text-[34px] font-medium leading-[1.15] tracking-tight text-foreground">
            {t(($) => $.step_work_tools.headline)}
          </h1>
          <p className="mt-3 max-w-[560px] text-sm leading-relaxed text-muted-foreground">
            {t(($) => $.step_work_tools.lede)}
          </p>
          <p className="mt-2 max-w-[560px] text-xs leading-relaxed text-muted-foreground/80">
            {t(($) => $.step_work_tools.binary_note)}
          </p>

          {/* No-CA fallback (issue #46): every preset below targets a
              perimeter host, and without the corporate CA bundle none of them
              can connect from this machine. Warns, never blocks — the save
              path stays open because tokens take effect once the bundle is
              installed. */}
          {perimeterCaMissing === true && (
            <p className="mt-3 flex items-start gap-1.5 text-xs text-warning" aria-live="polite">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_work_tools.perimeter_ca_missing)}
            </p>
          )}

          {/* Helper preparation status — informational, never blocking. */}
          {!agent && !agentError && (
            <p
              className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground"
              aria-live="polite"
            >
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
              {t(($) => $.step_work_tools.preparing_helper)}
            </p>
          )}
          {agentError && (
            <p className="mt-3 flex items-start gap-1.5 text-xs text-warning" aria-live="polite">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_work_tools.helper_error)}
            </p>
          )}
          {/* Redacted config: this viewer cannot read the stored credentials,
              so saving from here would destroy them (full-replace endpoint).
              Point at the Capabilities → MCP tab, same as the tab itself. */}
          {redacted && (
            <p className="mt-3 flex items-start gap-1.5 text-xs text-warning" aria-live="polite">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
              {t(($) => $.step_work_tools.redacted_note)}
            </p>
          )}

          <div className="mt-8 flex flex-col gap-4">
            {/* atlassian */}
            {visibleCardOrder.includes('atlassian') && (
              <WorkToolsCard preset="atlassian" badge={cardBadge('atlassian')}>
                {secretField(
                  'work-tools-jira-token',
                  t(($) => $.step_work_tools.presets.atlassian.jira_token_label),
                  t(($) => $.step_work_tools.presets.atlassian.token_placeholder),
                  credentials.atlassian.jiraToken,
                  (v) =>
                    patchCredentials((prev) => ({
                      ...prev,
                      atlassian: { ...prev.atlassian, jiraToken: v },
                    })),
                )}
                {secretField(
                  'work-tools-confluence-token',
                  t(($) => $.step_work_tools.presets.atlassian.confluence_token_label),
                  t(($) => $.step_work_tools.presets.atlassian.token_placeholder),
                  credentials.atlassian.confluenceToken,
                  (v) =>
                    patchCredentials((prev) => ({
                      ...prev,
                      atlassian: { ...prev.atlassian, confluenceToken: v },
                    })),
                )}
                <div className="flex items-center gap-2.5">
                  <Switch
                    id="work-tools-atlassian-ssl-verify"
                    disabled={inputsDisabled}
                    checked={credentials.atlassian.sslVerify}
                    onCheckedChange={(checked: boolean) =>
                      patchCredentials((prev) => ({
                        ...prev,
                        atlassian: { ...prev.atlassian, sslVerify: checked === true },
                      }))
                    }
                  />
                  <Label htmlFor="work-tools-atlassian-ssl-verify" className="text-[13px]">
                    {t(($) => $.step_work_tools.presets.atlassian.ssl_verify_label)}
                  </Label>
                </div>
              </WorkToolsCard>
            )}

            {/* outlook */}
            {visibleCardOrder.includes('outlook') && (
              <WorkToolsCard preset="outlook" badge={cardBadge('outlook')}>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="work-tools-ews-email" className="text-[13px]">
                    {t(($) => $.step_work_tools.presets.outlook.email_label)}
                  </Label>
                  {/* A mailbox address is not a secret — plain text input. */}
                  <Input
                    id="work-tools-ews-email"
                    type="text"
                    inputMode="email"
                    autoComplete="off"
                    spellCheck={false}
                    disabled={inputsDisabled}
                    placeholder={t(
                      ($) => $.step_work_tools.presets.outlook.email_placeholder,
                      { mailDomain: mailDomain || 'example.test' },
                    )}
                    value={credentials.outlook.email}
                    onChange={(event) =>
                      patchCredentials((prev) => ({
                        ...prev,
                        outlook: { email: event.target.value },
                      }))
                    }
                  />
                </div>
              </WorkToolsCard>
            )}

            {/* bitrix24 */}
            {visibleCardOrder.includes('bitrix24') && (
              <WorkToolsCard preset="bitrix24" badge={cardBadge('bitrix24')}>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="work-tools-b24-webhook" className="text-[13px]">
                    {t(($) => $.step_work_tools.presets.bitrix24.webhook_label)}
                  </Label>
                  <Input
                    id="work-tools-b24-webhook"
                    type="text"
                    autoComplete="off"
                    spellCheck={false}
                    disabled={inputsDisabled}
                    placeholder={t(($) => $.step_work_tools.presets.bitrix24.webhook_placeholder)}
                    value={credentials.bitrix24.webhook}
                    onChange={(event) =>
                      patchCredentials((prev) => ({
                        ...prev,
                        bitrix24: { ...prev.bitrix24, webhook: event.target.value },
                      }))
                    }
                  />
                  <p className="text-[11.5px] leading-[1.5] text-muted-foreground">
                    {t(($) => $.step_work_tools.presets.bitrix24.webhook_hint)}
                  </p>
                  {/* URL-slot validation: spaces or query/fragment markers would
                    corrupt the completed webhook URL, so the preset refuses to
                    complete until the value is clean. */}
                  {webhookInvalid && (
                    <p
                      aria-live="polite"
                      className="flex items-start gap-1.5 text-[11.5px] leading-[1.5] text-destructive"
                    >
                      <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                      {t(($) => $.step_work_tools.presets.bitrix24.webhook_invalid)}
                    </p>
                  )}
                </div>
                {secretField(
                  'work-tools-kb-token',
                  t(($) => $.step_work_tools.presets.bitrix24.kb_token_label),
                  t(($) => $.step_work_tools.presets.bitrix24.kb_token_placeholder),
                  credentials.bitrix24.kbToken,
                  (v) =>
                    patchCredentials((prev) => ({
                      ...prev,
                      bitrix24: { ...prev.bitrix24, kbToken: v },
                    })),
                )}
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="work-tools-kb-deployment" className="text-[13px]">
                    {t(($) => $.step_work_tools.presets.bitrix24.kb_deployment_label)}
                  </Label>
                  <Input
                    id="work-tools-kb-deployment"
                    type="text"
                    autoComplete="off"
                    spellCheck={false}
                    disabled={inputsDisabled}
                    placeholder={t(
                      ($) => $.step_work_tools.presets.bitrix24.kb_deployment_placeholder,
                    )}
                    value={credentials.bitrix24.kbDeployment}
                    onChange={(event) =>
                      patchCredentials((prev) => ({
                        ...prev,
                        bitrix24: {
                          ...prev.bitrix24,
                          kbDeployment: event.target.value,
                        },
                      }))
                    }
                  />
                </div>
              </WorkToolsCard>
            )}

            {/* fetch */}
            {visibleCardOrder.includes('fetch') && (
              <WorkToolsCard preset="fetch" badge={cardBadge('fetch')}>
                <div className="flex items-center gap-2.5">
                  <Switch
                    id="work-tools-fetch-enabled"
                    disabled={inputsDisabled}
                    checked={credentials.fetch.enabled}
                    onCheckedChange={(checked: boolean) =>
                      patchCredentials((prev) => ({
                        ...prev,
                        fetch: { enabled: checked === true },
                      }))
                    }
                  />
                  <Label htmlFor="work-tools-fetch-enabled" className="text-[13px]">
                    {t(($) => $.step_work_tools.presets.fetch.enable_label)}
                  </Label>
                </div>
              </WorkToolsCard>
            )}

            {/* mcp-gateway */}
            {visibleCardOrder.includes('mcp-gateway') && (
              <WorkToolsCard preset="mcp-gateway" badge={cardBadge('mcp-gateway')}>
                {secretField(
                  'work-tools-gateway-token',
                  t(($) => $.step_work_tools.presets.mcp_gateway.token_label),
                  t(($) => $.step_work_tools.presets.mcp_gateway.token_placeholder),
                  credentials['mcp-gateway'].bearerToken,
                  (v) =>
                    patchCredentials((prev) => ({
                      ...prev,
                      'mcp-gateway': { bearerToken: v },
                    })),
                )}
              </WorkToolsCard>
            )}
          </div>

          {/* Post-onboarding pointer: the same credentials live on in the
              agent's Capabilities → MCP tab. */}
          <p className="mt-6 text-xs leading-relaxed text-muted-foreground">
            {t(($) => $.step_work_tools.footer_note)}
          </p>

          <div className="mt-6 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
              {status.kind === 'error' ? (
                <span className="flex items-start gap-1.5 text-destructive">
                  <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
                  {status.message}
                </span>
              ) : (
                footerHint
              )}
            </span>
            <div className="flex items-center gap-2">
              {!saved && (
                <Button
                  size="lg"
                  variant="secondary"
                  disabled={saving}
                  onClick={() => void onFinish()}
                >
                  {t(($) => $.common.skip)}
                </Button>
              )}
              {saved ? (
                <Button size="lg" onClick={() => void onFinish()}>
                  {t(($) => $.common.continue)}
                  <ArrowRight className="h-4 w-4" />
                </Button>
              ) : (
                <Button size="lg" disabled={!canSave || saving} onClick={save}>
                  {saving && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
                  {saving ? t(($) => $.step_work_tools.saving) : t(($) => $.step_work_tools.save)}
                </Button>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

StepWorkTools.displayName = 'StepWorkTools';

function presetHasInput(name: HelperMcpPresetName, credentials: WorkToolsCredentials): boolean {
  switch (name) {
    case 'atlassian':
      return (
        credentials.atlassian.jiraToken.trim().length > 0 ||
        credentials.atlassian.confluenceToken.trim().length > 0
      );
    case 'outlook':
      return credentials.outlook.email.trim().length > 0;
    case 'bitrix24':
      return (
        credentials.bitrix24.webhook.trim().length > 0 ||
        credentials.bitrix24.kbToken.trim().length > 0 ||
        credentials.bitrix24.kbDeployment.trim().length > 0
      );
    case 'mcp-gateway':
      return credentials['mcp-gateway'].bearerToken.trim().length > 0;
    case 'fetch':
      return credentials.fetch.enabled === true;
  }
}
