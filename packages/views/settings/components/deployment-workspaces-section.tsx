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
import { describeApiFailure } from '@goosar/core/api';
import type {
  DeploymentWorkspaceEntry,
  DeploymentWorkspaceMemberEntry,
} from '@goosar/core/api/deployment-admin';
import type {
  UserConfigOverridePatch,
  UserConfigOverrideView,
  WorkspaceConfigPatch,
} from '@goosar/core/api/workspace-admin';
import {
  deploymentUserConfigOverrideOptions,
  deploymentWorkspaceConfigOptions,
  deploymentWorkspaceMembersOptions,
  deploymentWorkspaceOverridesOptions,
  deploymentWorkspacesOptions,
  useDeactivateDeploymentUser,
  useDeleteDeploymentUserConfigOverride,
  useReactivateDeploymentUser,
  useRevokeDeploymentUserSessions,
  useSetDeploymentUserConfigOverride,
  useUpdateDeploymentWorkspaceConfig,
} from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { describeServerFailure } from '../../common/server-error';
import { SettingsCard, SettingsRow, SettingsSection } from './settings-layout';

export function DeploymentWorkspacesSection() {
  const { t } = useT('settings');
  const workspacesQuery = useQuery(deploymentWorkspacesOptions());
  const [search, setSearch] = useState('');
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const directoryAnswer = answerOf(workspacesQuery);
  const list: DeploymentWorkspaceEntry[] = directoryAnswer.known ? directoryAnswer.value : [];
  const query = search.trim().toLowerCase();
  const filtered =
    query === ''
      ? list
      : list.filter(
          (ws) => ws.name.toLowerCase().includes(query) || ws.slug.toLowerCase().includes(query),
        );
  const selected = list.find((ws) => ws.id === selectedId) ?? null;

  return (
    <SettingsSection
      title={t(($) => $.deployment.workspaces.title)}
      description={t(($) => $.deployment.workspaces.description)}
    >
      <SettingsCard>
        <div className="px-4 py-3">
          <Input
            aria-label={t(($) => $.deployment.workspaces.search_aria)}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t(($) => $.deployment.workspaces.search_placeholder)}
            autoComplete="off"
          />
        </div>
        {/* An unanswered or refused read is NOT an empty deployment: the
            empty state is reachable only from a successful answer. */}
        {directoryAnswer.known === false && directoryAnswer.failed ? (
          <QueryFailure
            message={t(($) => $.deployment.workspaces.list_error)}
            error={directoryAnswer.error}
            onRetry={() => void workspacesQuery.refetch()}
            retryLabel={t(($) => $.deployment.workspaces.retry)}
          />
        ) : directoryAnswer.known === false ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.loading)}
          </p>
        ) : list.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.empty)}
          </p>
        ) : filtered.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.no_matches)}
          </p>
        ) : (
          filtered.map((ws, index) => {
            const identified = ws.id !== '';
            return (
              <div
                key={identified ? ws.id : `unidentified-${index}`}
                className="flex items-center gap-3 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{ws.name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {identified ? '/' + ws.slug : t(($) => $.deployment.workspaces.no_identity)}
                  </div>
                </div>
                {/* `total`, not `count`: i18next reserves `count` for plural
                    selection, and this is a fixed counter label ("Members: 3")
                    with no plural-sensitive wording in any of the five
                    bundles. */}
                <Badge variant="outline">
                  {t(($) => $.deployment.workspaces.member_count, {
                    total: ws.member_count,
                  })}
                </Badge>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={t(($) => $.deployment.workspaces.open_aria, {
                    name: ws.name,
                  })}
                  onClick={() => setSelectedId(ws.id)}
                  disabled={!identified}
                >
                  {t(($) => $.deployment.workspaces.open_action)}
                </Button>
              </div>
            );
          })
        )}
      </SettingsCard>

      {selected && (
        <DeploymentWorkspaceCard
          key={selected.id}
          workspace={selected}
          onClose={() => setSelectedId(null)}
        />
      )}
    </SettingsSection>
  );
}

function QueryFailure({
  message,
  error,
  onRetry,
  retryLabel,
}: {
  message: string;
  error: unknown;
  onRetry: () => void;
  retryLabel: string;
}) {
  const failure = describeApiFailure(error);
  return (
    <div className="space-y-2 px-4 py-3" role="alert">
      <p className="text-sm text-destructive">{message}</p>
      {failure.message !== null && (
        <p className="text-xs text-muted-foreground">{failure.message}</p>
      )}
      <Button variant="outline" size="sm" onClick={onRetry}>
        {retryLabel}
      </Button>
    </div>
  );
}

type Answer<T> = { known: true; value: T } | { known: false; failed: boolean; error: unknown };

interface QueryLikeAnswer<T> {
  data: T | undefined;
  error: unknown;
  isError: boolean;
  isSuccess: boolean;
}

function answerOf<T>(query: QueryLikeAnswer<T>): Answer<T> {
  if (query.isError === true) {
    return { known: false, failed: true, error: query.error };
  }
  if (query.isSuccess === true && query.data !== undefined) {
    return { known: true, value: query.data };
  }
  return { known: false, failed: false, error: null };
}

type OverrideKnowledge =
  { kind: 'present'; override: UserConfigOverrideView } | { kind: 'none' } | { kind: 'unknown' };

function DeploymentWorkspaceCard({
  workspace,
  onClose,
}: {
  workspace: DeploymentWorkspaceEntry;
  onClose: () => void;
}) {
  const { t } = useT('settings');
  const { t: tCommon } = useT('common');
  const describeChanges = useLayerChangeDescriber();
  const configQuery = useQuery(deploymentWorkspaceConfigOptions(workspace.id));
  const membersQuery = useQuery(deploymentWorkspaceMembersOptions(workspace.id));
  const overridesQuery = useQuery(deploymentWorkspaceOverridesOptions(workspace.id));
  const updateConfig = useUpdateDeploymentWorkspaceConfig(workspace.id);
  const setOverride = useSetDeploymentUserConfigOverride(workspace.id);
  const deleteOverride = useDeleteDeploymentUserConfigOverride(workspace.id);
  const deactivateUser = useDeactivateDeploymentUser();
  const revokeSessions = useRevokeDeploymentUserSessions();
  const reactivateUser = useReactivateDeploymentUser();

  const configAnswer = answerOf(configQuery);
  const config = configAnswer.known ? configAnswer.value : undefined;
  const membersAnswer = answerOf(membersQuery);
  const memberList: DeploymentWorkspaceMemberEntry[] = membersAnswer.known
    ? membersAnswer.value
    : [];
  const overridesAnswer = answerOf(overridesQuery);
  const overridesKnown = overridesAnswer.known;
  const overrideList: UserConfigOverrideView[] = overridesAnswer.known ? overridesAnswer.value : [];
  const listingHasAnonymousRow = overrideList.some((o) => o.user_id === '');

  const overrideFor = (userId: string): OverrideKnowledge => {
    if (!overridesKnown) return { kind: 'unknown' };
    if (userId === '') return { kind: 'unknown' };
    const found = overrideList.find((o) => o.user_id === userId);
    if (found !== undefined) return { kind: 'present', override: found };
    return listingHasAnonymousRow ? { kind: 'unknown' } : { kind: 'none' };
  };

  const [draftBaseUrl, setDraftBaseUrl] = useState<string | null>(null);
  const [draftModel, setDraftModel] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState('');
  const [configConfirmOpen, setConfigConfirmOpen] = useState(false);
  const [configError, setConfigError] = useState<string | null>(null);

  const [dialogMember, setDialogMember] = useState<DeploymentWorkspaceMemberEntry | null>(null);
  const [removeMember, setRemoveMember] = useState<DeploymentWorkspaceMemberEntry | null>(null);
  const [offboardMember, setOffboardMember] = useState<DeploymentWorkspaceMemberEntry | null>(null);
  const offboardPending = deactivateUser.isPending === true || reactivateUser.isPending === true;

  const serverBaseUrl = config?.llm_base_url ?? '';
  const serverModel = config?.llm_model ?? '';
  const baseUrl = draftBaseUrl ?? serverBaseUrl;
  const model = draftModel ?? serverModel;
  const configDirty = draftBaseUrl !== null || draftModel !== null || keyInput !== '';

  const memberDisplayName = (m: DeploymentWorkspaceMemberEntry): string => {
    if (m.name !== undefined && m.name !== '') return m.name;
    if (m.email !== undefined && m.email !== '') return m.email;
    if (m.user_id !== '') return m.user_id;
    return t(($) => $.deployment.workspaces.no_identity);
  };

  const buildConfigPatch = (): WorkspaceConfigPatch => {
    const patch: WorkspaceConfigPatch = {};
    if (draftBaseUrl !== null) patch.llm_base_url = draftBaseUrl;
    if (draftModel !== null) patch.llm_model = draftModel;
    if (keyInput !== '') patch.llm_api_key = keyInput;
    return patch;
  };

  const applyConfig = async () => {
    const patch = buildConfigPatch();
    if (Object.keys(patch).length === 0) {
      setConfigConfirmOpen(false);
      return;
    }
    setConfigError(null);
    try {
      await updateConfig.mutateAsync(patch);
      setDraftBaseUrl(null);
      setDraftModel(null);
      setKeyInput('');
      setConfigConfirmOpen(false);
      toast.success(t(($) => $.deployment.workspaces.toast_config_saved));
    } catch (err) {
      const failure = describeApiFailure(err);
      const fallback = t(($) => $.deployment.workspaces.toast_config_save_failed);
      setConfigError(failure.message ?? fallback);
      toast.error(failure.message ?? fallback);
    }
  };

  const applyOverride = async (
    member: DeploymentWorkspaceMemberEntry,
    patch: UserConfigOverridePatch,
  ) => {
    try {
      await setOverride.mutateAsync({ userId: member.user_id, patch });
      setDialogMember(null);
      toast.success(t(($) => $.deployment.workspaces.toast_override_saved));
    } catch (err) {
      const failure = describeApiFailure(err);
      toast.error(failure.message ?? t(($) => $.deployment.workspaces.toast_override_save_failed));
    }
  };

  const applyRemoveOverride = async (member: DeploymentWorkspaceMemberEntry) => {
    try {
      await deleteOverride.mutateAsync(member.user_id);
      setRemoveMember(null);
      toast.success(t(($) => $.deployment.workspaces.toast_override_removed));
    } catch (err) {
      const failure = describeApiFailure(err);
      toast.error(
        failure.message ?? t(($) => $.deployment.workspaces.toast_override_remove_failed),
      );
    }
  };

  const applyOffboarding = async (member: DeploymentWorkspaceMemberEntry) => {
    const reactivating = member.deactivated;
    try {
      if (reactivating) {
        await reactivateUser.mutateAsync(member.user_id);
      } else {
        await deactivateUser.mutateAsync(member.user_id);
      }
      setOffboardMember(null);
      toast.success(
        reactivating
          ? t(($) => $.deployment.workspaces.toast_reactivated)
          : t(($) => $.deployment.workspaces.toast_deactivated),
      );
    } catch (err) {
      const failure = describeServerFailure(
        tCommon,
        err,
        reactivating
          ? t(($) => $.deployment.workspaces.toast_reactivate_failed)
          : t(($) => $.deployment.workspaces.toast_deactivate_failed),
      );
      toast.error(
        failure.text,
        failure.detail === undefined ? undefined : { description: failure.detail },
      );
    }
  };

  return (
    <SettingsSection
      title={workspace.name}
      description={'/' + workspace.slug}
      action={
        <Button variant="ghost" size="sm" onClick={onClose}>
          {t(($) => $.deployment.workspaces.close_action)}
        </Button>
      }
    >
      {/* --- Workspace configuration (masked; PUT behind the L2 confirm) --- */}
      <SettingsCard>
        {/* An unread config must not render as "no base URL, no model, no
            key stored" — that is a claim about a workspace we could not
            read. Neither may an unfinished one: the empty form IS the
            sentence "nothing is stored here", and it is the frame every
            card open passes through (review #243, medium 4). */}
        {configAnswer.known === false && configAnswer.failed ? (
          <QueryFailure
            message={t(($) => $.deployment.workspaces.config_error)}
            error={configAnswer.error}
            onRetry={() => void configQuery.refetch()}
            retryLabel={t(($) => $.deployment.workspaces.retry)}
          />
        ) : configAnswer.known === false ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.loading)}
          </p>
        ) : (
          <>
            <SettingsRow
              label={t(($) => $.deployment.workspaces.config_title)}
              description={t(($) => $.deployment.workspaces.config_description)}
            >
              <Button
                size="sm"
                onClick={() => setConfigConfirmOpen(true)}
                disabled={updateConfig.isPending === true || !configDirty}
              >
                {t(($) => $.deployment.workspaces.config_save)}
              </Button>
            </SettingsRow>
            {configError !== null && (
              <p className="px-4 pb-2 text-sm text-destructive" role="alert">
                {configError}
              </p>
            )}
            <SettingsRow label={t(($) => $.deployment.workspaces.base_url_label)} size="text">
              <Input
                aria-label={t(($) => $.deployment.workspaces.config_base_url_aria)}
                value={baseUrl}
                onChange={(e) => setDraftBaseUrl(e.target.value)}
                autoComplete="off"
              />
            </SettingsRow>
            <SettingsRow label={t(($) => $.deployment.workspaces.model_label)} size="text">
              <Input
                aria-label={t(($) => $.deployment.workspaces.config_model_aria)}
                value={model}
                onChange={(e) => setDraftModel(e.target.value)}
                autoComplete="off"
              />
            </SettingsRow>
            <SettingsRow
              label={t(($) => $.deployment.workspaces.api_key_label)}
              description={
                config?.has_llm_api_key === true
                  ? t(($) => $.deployment.workspaces.api_key_set)
                  : undefined
              }
              size="text"
            >
              <Input
                aria-label={t(($) => $.deployment.workspaces.config_api_key_aria)}
                type="password"
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
                placeholder={t(($) => $.deployment.workspaces.api_key_placeholder)}
                autoComplete="off"
              />
            </SettingsRow>
          </>
        )}
      </SettingsCard>

      {/* --- Member roster (the user card material) --- */}
      <SettingsCard>
        {overridesAnswer.known === false && overridesAnswer.failed && (
          <QueryFailure
            message={t(($) => $.deployment.workspaces.overrides_error)}
            error={overridesAnswer.error}
            onRetry={() => void overridesQuery.refetch()}
            retryLabel={t(($) => $.deployment.workspaces.retry)}
          />
        )}
        {membersAnswer.known === false && membersAnswer.failed ? (
          <QueryFailure
            message={t(($) => $.deployment.workspaces.members_error)}
            error={membersAnswer.error}
            onRetry={() => void membersQuery.refetch()}
            retryLabel={t(($) => $.deployment.workspaces.retry)}
          />
        ) : membersAnswer.known === false ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.loading)}
          </p>
        ) : memberList.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.members_empty)}
          </p>
        ) : (
          memberList.map((member, index) => {
            const knowledge = overrideFor(member.user_id);
            const identified = member.user_id !== '';
            return (
              <div
                key={identified ? member.user_id : `unidentified-${index}`}
                className="flex items-center gap-3 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{memberDisplayName(member)}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {identified ? member.email : t(($) => $.deployment.workspaces.no_identity)}
                  </div>
                </div>
                {member.role !== '' && <Badge variant="outline">{member.role}</Badge>}
                {member.deactivated && (
                  <Badge variant="destructive">
                    {t(($) => $.deployment.workspaces.deactivated_badge)}
                  </Badge>
                )}
                <Badge variant={knowledge.kind === 'present' ? 'secondary' : 'outline'}>
                  {knowledge.kind === 'present'
                    ? t(($) => $.deployment.workspaces.override_badge)
                    : knowledge.kind === 'none'
                      ? t(($) => $.deployment.workspaces.no_override_badge)
                      : t(($) => $.deployment.workspaces.override_unknown_badge)}
                </Badge>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={t(($) => $.deployment.workspaces.override_aria, {
                    name: memberDisplayName(member),
                  })}
                  onClick={() => setDialogMember(member)}
                  disabled={!identified}
                >
                  {t(($) => $.deployment.workspaces.override_action)}
                </Button>
                {knowledge.kind === 'present' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t(($) => $.deployment.workspaces.override_remove_aria, {
                      name: memberDisplayName(member),
                    })}
                    onClick={() => setRemoveMember(member)}
                    disabled={deleteOverride.isPending === true}
                  >
                    {t(($) => $.deployment.workspaces.override_remove_action)}
                  </Button>
                )}
                {/* Ends this person's sessions without blocking the account
                    (#391). No typed-phrase dialog: the person signs in again,
                    so the action is recoverable by them alone. */}
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t(($) => $.deployment.workspaces.revoke_sessions_aria, {
                    name: memberDisplayName(member),
                  })}
                  disabled={!identified || revokeSessions.isPending === true}
                  onClick={async () => {
                    try {
                      const result = await revokeSessions.mutateAsync(member.user_id ?? '');
                      toast.success(
                        t(($) => $.deployment.workspaces.toast_sessions_revoked, {
                          count: result.revoked,
                        }),
                      );
                    } catch {
                      toast.error(t(($) => $.deployment.workspaces.toast_revoke_failed));
                    }
                  }}
                >
                  {t(($) => $.deployment.workspaces.revoke_sessions_action)}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={
                    member.deactivated
                      ? t(($) => $.deployment.workspaces.reactivate_aria, {
                          name: memberDisplayName(member),
                        })
                      : t(($) => $.deployment.workspaces.deactivate_aria, {
                          name: memberDisplayName(member),
                        })
                  }
                  onClick={() => setOffboardMember(member)}
                  disabled={!identified || offboardPending}
                >
                  {member.deactivated
                    ? t(($) => $.deployment.workspaces.reactivate_action)
                    : t(($) => $.deployment.workspaces.deactivate_action)}
                </Button>
              </div>
            );
          })
        )}
      </SettingsCard>

      {/* §3 L2: plain confirm before the workspace-config PUT. */}
      <AlertDialog open={configConfirmOpen} onOpenChange={setConfigConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.deployment.workspaces.config_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.workspaces.config_confirm_description, {
                name: workspace.name,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ChangeSummary
            changes={describeChanges({
              draftBaseUrl,
              draftModel,
              keyTouched: keyInput !== '',
            })}
          />
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.deployment.workspaces.confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={applyConfig} disabled={updateConfig.isPending === true}>
              {t(($) => $.deployment.workspaces.confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* §3 L2: removing an override is its own confirm. */}
      <AlertDialog
        open={removeMember !== null}
        onOpenChange={(open) => !open && setRemoveMember(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.deployment.workspaces.override_remove_confirm_title, {
                name: removeMember ? memberDisplayName(removeMember) : '',
              })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.deployment.workspaces.override_remove_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.deployment.workspaces.confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (removeMember) void applyRemoveOverride(removeMember);
              }}
              disabled={deleteOverride.isPending === true}
            >
              {t(($) => $.deployment.workspaces.override_remove_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Account offboarding is deployment-wide, so it confirms on its own. */}
      <AlertDialog
        open={offboardMember !== null}
        onOpenChange={(open) => !open && setOffboardMember(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {offboardMember?.deactivated === true
                ? t(($) => $.deployment.workspaces.reactivate_confirm_title, {
                    name: memberDisplayName(offboardMember),
                  })
                : t(($) => $.deployment.workspaces.deactivate_confirm_title, {
                    name: offboardMember ? memberDisplayName(offboardMember) : '',
                  })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {offboardMember?.deactivated === true
                ? t(($) => $.deployment.workspaces.reactivate_confirm_description)
                : t(($) => $.deployment.workspaces.deactivate_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.deployment.workspaces.confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (offboardMember) void applyOffboarding(offboardMember);
              }}
              disabled={offboardPending}
            >
              {offboardMember?.deactivated === true
                ? t(($) => $.deployment.workspaces.reactivate_confirm)
                : t(($) => $.deployment.workspaces.deactivate_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {dialogMember && (
        <DeploymentOverrideDialog
          workspaceId={workspace.id}
          userId={dialogMember.user_id}
          memberName={memberDisplayName(dialogMember)}
          pending={setOverride.isPending === true}
          onClose={() => setDialogMember(null)}
          onConfirmedSave={(patch) => applyOverride(dialogMember, patch)}
        />
      )}
    </SettingsSection>
  );
}

interface LayerDrafts {
  draftBaseUrl: string | null;
  draftModel: string | null;
  keyTouched: boolean;
}

function useLayerChangeDescriber(): (drafts: LayerDrafts) => string[] {
  const { t } = useT('settings');
  return (drafts: LayerDrafts): string[] => {
    const changes: string[] = [];
    const nextBaseUrl = drafts.draftBaseUrl;
    if (nextBaseUrl !== null) {
      changes.push(
        nextBaseUrl === ''
          ? t(($) => $.deployment.workspaces.change_base_url_cleared)
          : t(($) => $.deployment.workspaces.change_base_url, {
              value: nextBaseUrl,
            }),
      );
    }
    const nextModel = drafts.draftModel;
    if (nextModel !== null) {
      changes.push(
        nextModel === ''
          ? t(($) => $.deployment.workspaces.change_model_cleared)
          : t(($) => $.deployment.workspaces.change_model, {
              value: nextModel,
            }),
      );
    }
    if (drafts.keyTouched) {
      changes.push(t(($) => $.deployment.workspaces.change_api_key));
    }
    return changes;
  };
}

function ChangeSummary({ changes }: { changes: string[] }) {
  if (changes.length === 0) return null;
  return (
    <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
      {changes.map((change) => (
        <li key={change}>{change}</li>
      ))}
    </ul>
  );
}

function DeploymentOverrideDialog({
  workspaceId,
  userId,
  memberName,
  pending,
  onClose,
  onConfirmedSave,
}: {
  workspaceId: string;
  userId: string;
  memberName: string;
  pending: boolean;
  onClose: () => void;
  onConfirmedSave: (patch: UserConfigOverridePatch) => Promise<void>;
}) {
  const { t } = useT('settings');
  const describeChanges = useLayerChangeDescriber();
  const overrideQuery = useQuery(deploymentUserConfigOverrideOptions(workspaceId, userId));
  const overrideAnswer = answerOf<UserConfigOverrideView | null>(overrideQuery);
  const override: UserConfigOverrideView | null = overrideAnswer.known
    ? overrideAnswer.value
    : null;

  const [draftBaseUrl, setDraftBaseUrl] = useState<string | null>(null);
  const [draftModel, setDraftModel] = useState<string | null>(null);
  const [keyInput, setKeyInput] = useState('');
  const [confirmOpen, setConfirmOpen] = useState(false);

  const serverBaseUrl = override?.llm_base_url ?? '';
  const serverModel = override?.llm_model ?? '';
  const baseUrl = draftBaseUrl ?? serverBaseUrl;
  const model = draftModel ?? serverModel;

  const buildPatch = (): UserConfigOverridePatch => {
    const patch: UserConfigOverridePatch = {};
    if (draftBaseUrl !== null) patch.llm_base_url = draftBaseUrl;
    if (draftModel !== null) patch.llm_model = draftModel;
    if (keyInput !== '') patch.llm_api_key = keyInput;
    return patch;
  };

  const changes = describeChanges({
    draftBaseUrl,
    draftModel,
    keyTouched: keyInput !== '',
  });

  const requestSave = () => {
    if (changes.length === 0) {
      onClose();
      return;
    }
    setConfirmOpen(true);
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.deployment.workspaces.override_dialog_title, {
              name: memberName,
            })}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.deployment.workspaces.override_dialog_description)}
          </DialogDescription>
        </DialogHeader>
        {!overrideAnswer.known && overrideAnswer.failed !== true && (
          <p className="text-sm text-muted-foreground">
            {t(($) => $.deployment.workspaces.override_not_read_yet)}
          </p>
        )}
        {overrideQuery.isError === true && (
          <QueryFailure
            message={t(($) => $.deployment.workspaces.override_read_error)}
            error={overrideQuery.error}
            onRetry={() => void overrideQuery.refetch()}
            retryLabel={t(($) => $.deployment.workspaces.retry)}
          />
        )}
        <div className="space-y-3">
          <Input
            aria-label={t(($) => $.deployment.workspaces.override_base_url_aria)}
            value={baseUrl}
            onChange={(e) => setDraftBaseUrl(e.target.value)}
            placeholder={t(($) => $.deployment.workspaces.base_url_label)}
            autoComplete="off"
          />
          <Input
            aria-label={t(($) => $.deployment.workspaces.override_model_aria)}
            value={model}
            onChange={(e) => setDraftModel(e.target.value)}
            placeholder={t(($) => $.deployment.workspaces.model_label)}
            autoComplete="off"
          />
          <div className="space-y-1">
            <Input
              aria-label={t(($) => $.deployment.workspaces.override_api_key_aria)}
              type="password"
              value={keyInput}
              onChange={(e) => setKeyInput(e.target.value)}
              placeholder={t(($) => $.deployment.workspaces.api_key_placeholder)}
              autoComplete="off"
            />
            {override?.has_llm_api_key === true && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.deployment.workspaces.api_key_set)}
              </p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={pending}>
            {t(($) => $.deployment.workspaces.confirm_cancel)}
          </Button>
          <Button onClick={requestSave} disabled={pending}>
            {t(($) => $.deployment.workspaces.override_save)}
          </Button>
        </DialogFooter>

        {/* §3 L2: the confirm between Save and the PUT — and it NAMES the
            destructive half, because "the value will be cleared" is not
            something an admin should have to deduce. */}
        <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.deployment.workspaces.override_confirm_title, {
                  name: memberName,
                })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.deployment.workspaces.override_confirm_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <ChangeSummary changes={changes} />
            <AlertDialogFooter>
              <AlertDialogCancel>
                {t(($) => $.deployment.workspaces.confirm_cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  setConfirmOpen(false);
                  void onConfirmedSave(buildPatch());
                }}
                disabled={pending}
              >
                {t(($) => $.deployment.workspaces.confirm_apply)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  );
}
