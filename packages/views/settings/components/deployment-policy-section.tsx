'use client';

import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@goosar/ui/components/ui/alert-dialog';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { Switch } from '@goosar/ui/components/ui/switch';
import { isImeComposing } from '@goosar/core/utils';
import {
  isMcpKillSwitchActive,
  MCP_POLICY_WILDCARD,
  type DeploymentPolicyDoc,
  type DeploymentPolicyLlm,
} from '@goosar/core/api/deployment-admin';
import { deploymentPolicyOptions, useUpdateDeploymentPolicy } from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow, SettingsSection } from './settings-layout';

export const MCP_KILL_CONFIRM_PHRASE = 'DISABLE';

export function DeploymentPolicySection() {
  const { t } = useT('settings');
  const { data: view } = useQuery(deploymentPolicyOptions());
  const updatePolicy = useUpdateDeploymentPolicy();

  const doc: DeploymentPolicyDoc = view?.policy ?? {};
  const killActive = isMcpKillSwitchActive(doc);

  const [draftBaseUrl, setDraftBaseUrl] = useState<string | null>(null);
  const [draftModel, setDraftModel] = useState<string | null>(null);
  const [llmConfirmOpen, setLlmConfirmOpen] = useState(false);
  const [killDialogOpen, setKillDialogOpen] = useState(false);
  const [unkillConfirmOpen, setUnkillConfirmOpen] = useState(false);

  const serverBaseUrl = doc.llm?.base_url ?? '';
  const serverModel = doc.llm?.model ?? '';
  const baseUrl = draftBaseUrl ?? serverBaseUrl;
  const model = draftModel ?? serverModel;
  const llmDirty = baseUrl !== serverBaseUrl || model !== serverModel;

  const putDoc = async (next: DeploymentPolicyDoc, onSaved?: () => void) => {
    try {
      await updatePolicy.mutateAsync(next);
      onSaved?.();
      toast.success(t(($) => $.deployment.policy.toast_saved));
    } catch {
      toast.error(t(($) => $.deployment.policy.toast_save_failed));
    }
  };

  const applyLlm = async () => {
    const llm: DeploymentPolicyLlm = { ...(doc.llm ?? {}) };
    if (baseUrl !== '') llm.base_url = baseUrl;
    else delete llm.base_url;
    if (model !== '') llm.model = model;
    else delete llm.model;
    const next: DeploymentPolicyDoc = { ...doc };
    if (Object.keys(llm).length > 0) {
      next.llm = llm;
    } else {
      delete next.llm;
    }
    await putDoc(next, () => {
      setDraftBaseUrl(null);
      setDraftModel(null);
      setLlmConfirmOpen(false);
    });
  };

  const applyKillSwitch = async () => {
    const next: DeploymentPolicyDoc = {
      ...doc,
      mcp: { ...(doc.mcp ?? {}), [MCP_POLICY_WILDCARD]: { enabled: false, locked: true } },
    };
    await putDoc(next, () => setKillDialogOpen(false));
  };

  const liftKillSwitch = async () => {
    const rest = { ...(doc.mcp ?? {}) };
    delete rest[MCP_POLICY_WILDCARD];
    const next: DeploymentPolicyDoc = { ...doc };
    if (Object.keys(rest).length > 0) {
      next.mcp = rest;
    } else {
      delete next.mcp;
    }
    await putDoc(next, () => setUnkillConfirmOpen(false));
  };

  const namedMcpEntries = Object.entries(doc.mcp ?? {}).filter(
    ([name]) => name !== MCP_POLICY_WILDCARD,
  );

  return (
    <SettingsSection
      title={t(($) => $.deployment.policy.title)}
      description={t(($) => $.deployment.policy.description)}
      action={
        <Button
          size="sm"
          onClick={() => setLlmConfirmOpen(true)}
          disabled={updatePolicy.isPending || !llmDirty}
        >
          {t(($) => $.deployment.policy.save)}
        </Button>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.deployment.policy.base_url_label)} size="text">
          <Input
            aria-label={t(($) => $.deployment.policy.base_url_label)}
            value={baseUrl}
            onChange={(e) => setDraftBaseUrl(e.target.value)}
            placeholder={t(($) => $.deployment.policy.base_url_placeholder)}
            autoComplete="off"
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.deployment.policy.model_label)} size="text">
          <Input
            aria-label={t(($) => $.deployment.policy.model_label)}
            value={model}
            onChange={(e) => setDraftModel(e.target.value)}
            placeholder={t(($) => $.deployment.policy.model_placeholder)}
            autoComplete="off"
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.deployment.policy.kill_title)}
          description={t(($) => $.deployment.policy.kill_description)}
        >
          <Switch
            aria-label={t(($) => $.deployment.policy.kill_switch_aria)}
            checked={killActive}
            disabled={updatePolicy.isPending}
            onCheckedChange={(next) => {
              if (next === true) {
                setKillDialogOpen(true);
              } else {
                setUnkillConfirmOpen(true);
              }
            }}
          />
        </SettingsRow>
        {namedMcpEntries.length > 0 && (
          <SettingsRow label={t(($) => $.deployment.policy.mcp_entries_label)} align="start">
            <div className="flex flex-wrap justify-end gap-1.5">
              {namedMcpEntries.map(([name, entry]) => (
                <Badge key={name} variant="outline" className="font-mono">
                  {name}
                  {entry.enabled === false
                    ? ` · ${t(($) => $.deployment.policy.mcp_entry_disabled)}`
                    : ''}
                  {entry.locked === true
                    ? ` · ${t(($) => $.deployment.policy.mcp_entry_locked)}`
                    : ''}
                </Badge>
              ))}
            </div>
          </SettingsRow>
        )}
      </SettingsCard>

      {/* §3 L2: plain confirm before the LLM policy PUT. */}
      <AlertDialog open={llmConfirmOpen} onOpenChange={setLlmConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.deployment.policy.llm_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.policy.llm_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.deployment.policy.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={applyLlm} disabled={updatePolicy.isPending}>
              {t(($) => $.deployment.policy.confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* §3 L2: lifting the kill switch is a plain confirm. */}
      <AlertDialog open={unkillConfirmOpen} onOpenChange={setUnkillConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.deployment.policy.unkill_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.policy.unkill_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.deployment.policy.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={liftKillSwitch} disabled={updatePolicy.isPending}>
              {t(($) => $.deployment.policy.unkill_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <McpKillDialog
        open={killDialogOpen}
        onOpenChange={setKillDialogOpen}
        loading={updatePolicy.isPending}
        onConfirm={applyKillSwitch}
      />
    </SettingsSection>
  );
}

function McpKillDialog({
  open,
  onOpenChange,
  loading,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  onConfirm: () => void;
}) {
  const { t } = useT('settings');
  const [typed, setTyped] = useState('');
  const matched = typed === MCP_KILL_CONFIRM_PHRASE;

  useEffect(() => {
    setTyped('');
  }, [open]);

  const submit = () => {
    if (!matched || loading) return;
    onConfirm();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.deployment.policy.kill_dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.deployment.policy.kill_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="mcp-kill-confirm" className="text-xs">
            {t(($) => $.deployment.policy.kill_type_prefix)}{' '}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
              {MCP_KILL_CONFIRM_PHRASE}
            </code>{' '}
            {t(($) => $.deployment.policy.kill_type_suffix)}
          </Label>
          <Input
            id="mcp-kill-confirm"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === 'Enter') {
                e.preventDefault();
                submit();
              }
            }}
            placeholder={MCP_KILL_CONFIRM_PHRASE}
            autoFocus
            disabled={loading}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            {t(($) => $.deployment.policy.confirm_cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={submit}
            disabled={!matched || loading}
          >
            {t(($) => $.deployment.policy.kill_confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
