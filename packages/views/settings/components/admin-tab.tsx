'use client';

import { useState } from 'react';
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
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { useAuthStore } from '@goosar/core/auth';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { memberListOptions } from '@goosar/core/workspace/queries';
import {
  useUpdateWorkspaceConfig,
  workspaceConfigOptions,
} from '@goosar/core/workspace/admin-config';
import type { WorkspaceConfigPatch } from '@goosar/core/api/workspace-admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from './settings-layout';
import { AdminMcpDefaultsSection } from './admin-mcp-defaults-section';
import { AdminOverridesSection } from './admin-overrides-section';
import { AdminPinsSection } from './admin-pins-section';

export function AdminTab() {
  const { t } = useT('settings');
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id;
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId ?? ''),
    enabled: !!wsId,
  });

  const viewer = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = viewer?.role === 'owner' || viewer?.role === 'admin';

  if (!wsId || !canManage) return null;

  return (
    <SettingsTab title={t(($) => $.admin.tab_title)} description={t(($) => $.admin.description)}>
      <AdminLlmSection wsId={wsId} />
      <AdminMcpDefaultsSection wsId={wsId} />
      <AdminOverridesSection wsId={wsId} members={members} />
      <AdminPinsSection wsId={wsId} />
    </SettingsTab>
  );
}

function AdminLlmSection({ wsId }: { wsId: string }) {
  const { t } = useT('settings');
  const { data: config } = useQuery(workspaceConfigOptions(wsId));
  const updateConfig = useUpdateWorkspaceConfig(wsId);

  const [draftBaseUrl, setDraftBaseUrl] = useState<string | null>(null);
  const [draftModel, setDraftModel] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState('');
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [clearConfirmOpen, setClearConfirmOpen] = useState(false);

  const serverBaseUrl = config?.llm_base_url ?? '';
  const serverModel = config?.llm_model ?? '';
  const hasKey = config?.has_llm_api_key === true;

  const buildPatch = (): WorkspaceConfigPatch => {
    const patch: WorkspaceConfigPatch = {};
    if (draftBaseUrl !== null && draftBaseUrl !== serverBaseUrl) {
      patch.llm_base_url = draftBaseUrl;
    }
    if (draftModel !== null && draftModel !== serverModel) {
      patch.llm_model = draftModel;
    }
    if (keyInput !== '') {
      patch.llm_api_key = keyInput;
    }
    return patch;
  };

  const requestSave = () => {
    if (Object.keys(buildPatch()).length === 0) return;
    setConfirmOpen(true);
  };

  const applySave = async () => {
    const patch = buildPatch();
    try {
      await updateConfig.mutateAsync(patch);
      setDraftBaseUrl(null);
      setDraftModel(null);
      setKeyInput('');
      setConfirmOpen(false);
      toast.success(t(($) => $.admin.llm.toast_saved));
    } catch {
      toast.error(t(($) => $.admin.llm.toast_save_failed));
    }
  };

  const applyClearKey = async () => {
    try {
      await updateConfig.mutateAsync({ llm_api_key: '' });
      setClearConfirmOpen(false);
      toast.success(t(($) => $.admin.llm.toast_saved));
    } catch {
      toast.error(t(($) => $.admin.llm.toast_save_failed));
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.admin.llm.title)}
      description={t(($) => $.admin.llm.description)}
      action={
        <Button size="sm" onClick={requestSave} disabled={updateConfig.isPending}>
          {t(($) => $.admin.llm.save)}
        </Button>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.admin.llm.base_url_label)} size="text">
          <Input
            aria-label={t(($) => $.admin.llm.base_url_label)}
            value={draftBaseUrl ?? serverBaseUrl}
            onChange={(e) => setDraftBaseUrl(e.target.value)}
            placeholder={t(($) => $.admin.llm.base_url_placeholder)}
            autoComplete="off"
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.admin.llm.model_label)} size="text">
          <Input
            aria-label={t(($) => $.admin.llm.model_label)}
            value={draftModel ?? serverModel}
            onChange={(e) => setDraftModel(e.target.value)}
            placeholder={t(($) => $.admin.llm.model_placeholder)}
            autoComplete="off"
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.admin.llm.api_key_label)}
          description={t(($) => $.admin.llm.api_key_hint)}
          size="text"
          align="start"
        >
          <div className="flex w-full flex-col gap-2">
            <Input
              aria-label={t(($) => $.admin.llm.api_key_label)}
              type="password"
              value={keyInput}
              onChange={(e) => setKeyInput(e.target.value)}
              placeholder={t(($) => $.admin.llm.api_key_placeholder)}
              autoComplete="off"
            />
            <div className="flex items-center gap-2">
              <Badge variant={hasKey ? 'secondary' : 'outline'}>
                {hasKey ? t(($) => $.admin.llm.api_key_set) : t(($) => $.admin.llm.api_key_not_set)}
              </Badge>
              {hasKey && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setClearConfirmOpen(true)}
                  disabled={updateConfig.isPending}
                >
                  {t(($) => $.admin.llm.api_key_clear)}
                </Button>
              )}
            </div>
          </div>
        </SettingsRow>
      </SettingsCard>

      {/* §3 L2: plain confirm before the workspace config PUT (issue #244). */}
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.admin.llm.confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.admin.llm.confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.admin.llm.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={applySave} disabled={updateConfig.isPending}>
              {t(($) => $.admin.llm.confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* §3 L2: the same confirm on the way out — clearing the workspace key
          strands every member's agents until a new one is set (issue #244). */}
      <AlertDialog open={clearConfirmOpen} onOpenChange={setClearConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.admin.llm.clear_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.admin.llm.clear_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.admin.llm.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={applyClearKey}
              disabled={updateConfig.isPending}
            >
              {t(($) => $.admin.llm.clear_confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
