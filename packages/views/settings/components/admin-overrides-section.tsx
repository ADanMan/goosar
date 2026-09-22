'use client';

import { useState } from 'react';
import { toast } from 'sonner';
import { useQueries } from '@tanstack/react-query';
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import {
  useDeleteUserConfigOverride,
  userConfigOverrideOptions,
  useSetUserConfigOverride,
} from '@goosar/core/workspace/admin-config';
import type {
  UserConfigOverridePatch,
  UserConfigOverrideView,
} from '@goosar/core/api/workspace-admin';
import type { MemberWithUser } from '@goosar/core/types';
import { useT } from '../../i18n';
import { memberLabel } from './member-label';
import { SettingsCard, SettingsSection } from './settings-layout';

export function AdminOverridesSection({
  wsId,
  members,
}: {
  wsId: string;
  members: MemberWithUser[];
}) {
  const { t } = useT('settings');
  const overrideQueries = useQueries({
    queries: members.map((m) => userConfigOverrideOptions(wsId, m.user_id)),
  });
  const setOverride = useSetUserConfigOverride(wsId);
  const deleteOverride = useDeleteUserConfigOverride(wsId);

  const [dialogMember, setDialogMember] = useState<MemberWithUser | null>(null);
  const [removeMember, setRemoveMember] = useState<MemberWithUser | null>(null);

  const overrideFor = (userId: string): UserConfigOverrideView | null => {
    const idx = members.findIndex((m) => m.user_id === userId);
    if (idx < 0) return null;
    return (overrideQueries[idx]?.data ?? null) as UserConfigOverrideView | null;
  };

  const confirmRemoveOverride = async (member: MemberWithUser) => {
    try {
      await deleteOverride.mutateAsync(member.user_id);
      setRemoveMember(null);
      toast.success(t(($) => $.admin.overrides.toast_removed));
    } catch {
      toast.error(t(($) => $.admin.overrides.toast_remove_failed));
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.admin.overrides.title)}
      description={t(($) => $.admin.overrides.description)}
    >
      <SettingsCard>
        {members.map((member) => (
          <OverrideRow
            key={member.user_id}
            member={member}
            override={overrideFor(member.user_id)}
            busy={deleteOverride.isPending}
            onConfigure={() => setDialogMember(member)}
            onRemove={() => setRemoveMember(member)}
          />
        ))}
      </SettingsCard>
      {/* §3 L2: the confirm names the member whose machines lose the
          override — and, with it, any key substituted only there. The name
          goes through memberLabel, so an empty display name falls back to the
          email instead of leaving the admin confirming a blank. */}
      <AlertDialog
        open={removeMember !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveMember(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            {/* Display names and emails are unbounded z.string() fields and
                land inside a max-w-sm popup — same wrapping the MCP section and
                the typed dialogs already got. */}
            <AlertDialogTitle className="break-words">
              {t(($) => $.admin.overrides.remove_confirm_title, {
                name: removeMember ? memberLabel(removeMember) : '',
              })}
            </AlertDialogTitle>
            <AlertDialogDescription className="break-words">
              {t(($) => $.admin.overrides.remove_confirm_description, {
                name: removeMember ? memberLabel(removeMember) : '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.admin.overrides.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (removeMember) void confirmRemoveOverride(removeMember);
              }}
              disabled={deleteOverride.isPending}
            >
              {t(($) => $.admin.overrides.remove_confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {dialogMember && (
        <OverrideDialog
          member={dialogMember}
          override={overrideFor(dialogMember.user_id)}
          pending={setOverride.isPending}
          onClose={() => setDialogMember(null)}
          onSave={async (patch) => {
            try {
              await setOverride.mutateAsync({
                userId: dialogMember.user_id,
                patch,
              });
              setDialogMember(null);
              toast.success(t(($) => $.admin.overrides.toast_saved));
            } catch {
              toast.error(t(($) => $.admin.overrides.toast_save_failed));
            }
          }}
        />
      )}
    </SettingsSection>
  );
}

function OverrideRow({
  member,
  override,
  busy,
  onConfigure,
  onRemove,
}: {
  member: MemberWithUser;
  override: UserConfigOverrideView | null;
  busy: boolean;
  onConfigure: () => void;
  onRemove: () => void;
}) {
  const { t } = useT('settings');
  const hasOverride = override !== null;
  const mcpCount = Object.keys(override?.mcp_overrides ?? {}).length;

  return (
    <div className="flex items-center gap-3 px-4 py-3" data-member-row>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{member.name}</div>
        <div className="truncate text-xs text-muted-foreground">{member.email}</div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {!hasOverride && <Badge variant="outline">{t(($) => $.admin.overrides.none_badge)}</Badge>}
        {override?.llm_base_url ? (
          <Badge variant="secondary">{t(($) => $.admin.overrides.base_url_badge)}</Badge>
        ) : null}
        {override?.llm_model ? (
          <Badge variant="secondary">{t(($) => $.admin.overrides.model_badge)}</Badge>
        ) : null}
        {override?.has_llm_api_key === true && (
          <Badge variant="secondary">{t(($) => $.admin.overrides.api_key_badge)}</Badge>
        )}
        {mcpCount > 0 && (
          <Badge variant="secondary">
            {t(($) => $.admin.overrides.mcp_badge, { count: mcpCount })}
          </Badge>
        )}
      </div>
      <Button
        variant="outline"
        size="sm"
        aria-label={t(($) => $.admin.overrides.configure_aria, {
          name: memberLabel(member),
        })}
        onClick={onConfigure}
      >
        {t(($) => $.admin.overrides.configure_action)}
      </Button>
      {hasOverride && (
        <Button
          variant="ghost"
          size="sm"
          aria-label={t(($) => $.admin.overrides.remove_aria, {
            name: memberLabel(member),
          })}
          onClick={onRemove}
          disabled={busy}
        >
          {t(($) => $.admin.overrides.remove_action)}
        </Button>
      )}
    </div>
  );
}

function OverrideDialog({
  member,
  override,
  pending,
  onClose,
  onSave,
}: {
  member: MemberWithUser;
  override: UserConfigOverrideView | null;
  pending: boolean;
  onClose: () => void;
  onSave: (patch: UserConfigOverridePatch) => Promise<void>;
}) {
  const { t } = useT('settings');
  const initialBaseUrl = override?.llm_base_url ?? '';
  const initialModel = override?.llm_model ?? '';
  const [baseUrl, setBaseUrl] = useState(initialBaseUrl);
  const [model, setModel] = useState(initialModel);
  const [keyInput, setKeyInput] = useState('');
  const [confirmingPatch, setConfirmingPatch] = useState<UserConfigOverridePatch | null>(null);

  const save = () => {
    const patch: UserConfigOverridePatch = {};
    if (baseUrl !== initialBaseUrl) patch.llm_base_url = baseUrl;
    if (model !== initialModel) patch.llm_model = model;
    if (keyInput !== '') patch.llm_api_key = keyInput;
    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    setConfirmingPatch(patch);
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="break-words">
            {t(($) => $.admin.overrides.dialog_title, {
              name: memberLabel(member),
            })}
          </DialogTitle>
          <DialogDescription>{t(($) => $.admin.overrides.dialog_description)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <Input
            aria-label={t(($) => $.admin.llm.base_url_label)}
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder={t(($) => $.admin.llm.base_url_placeholder)}
            autoComplete="off"
          />
          <Input
            aria-label={t(($) => $.admin.llm.model_label)}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder={t(($) => $.admin.llm.model_placeholder)}
            autoComplete="off"
          />
          <div className="space-y-1">
            <Input
              aria-label={t(($) => $.admin.llm.api_key_label)}
              type="password"
              value={keyInput}
              onChange={(e) => setKeyInput(e.target.value)}
              placeholder={t(($) => $.admin.llm.api_key_placeholder)}
              autoComplete="off"
            />
            {override?.has_llm_api_key === true && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.admin.llm.api_key_set)}
                {' · '}
                {t(($) => $.admin.llm.api_key_hint)}
              </p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.admin.overrides.cancel)}
          </Button>
          <Button onClick={save} disabled={pending}>
            {t(($) => $.admin.overrides.save)}
          </Button>
        </DialogFooter>
      </DialogContent>

      {/* §3 L2: the confirm step names the member whose machines the
          override (and a substituted key) will reach. "Back" returns to the
          form with the drafts intact. */}
      <AlertDialog
        open={confirmingPatch !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmingPatch(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="break-words">
              {t(($) => $.admin.overrides.confirm_title, {
                name: memberLabel(member),
              })}
            </AlertDialogTitle>
            <AlertDialogDescription className="break-words">
              {t(($) => $.admin.overrides.confirm_description, {
                name: memberLabel(member),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.admin.overrides.confirm_back)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirmingPatch) void onSave(confirmingPatch);
              }}
              disabled={pending}
            >
              {t(($) => $.admin.overrides.confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}
