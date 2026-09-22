'use client';

import { useState } from 'react';
import {
  Crown,
  Shield,
  ShieldCheck,
  User,
  Plus,
  MoreHorizontal,
  UserMinus,
  Clock,
  X,
  Mail,
} from 'lucide-react';
import { ActorAvatar } from '../../common/actor-avatar';
import type { MemberWithUser, MemberRole, Invitation } from '@goosar/core/types';
import { Input } from '@goosar/ui/components/ui/input';
import { Button } from '@goosar/ui/components/ui/button';
import { Card, CardContent } from '@goosar/ui/components/ui/card';
import { Badge } from '@goosar/ui/components/ui/badge';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from '@goosar/ui/components/ui/alert-dialog';
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@goosar/ui/components/ui/select';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from '@goosar/ui/components/ui/dropdown-menu';
import { toast } from 'sonner';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { isPerimeterDeliveryProfile, useConfigStore } from '@goosar/core/config';
import {
  canGrantPerimeterAccess,
  canManageMembers,
  type PermissionContext,
} from '@goosar/core/permissions';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useCurrentWorkspace } from '@goosar/core/paths';
import {
  memberListOptions,
  invitationListOptions,
  workspaceKeys,
} from '@goosar/core/workspace/queries';
import { api } from '@goosar/core/api';
import { useT } from '../../i18n';
import { describeServerFailure } from '../../common/server-error';
import { memberLabel } from './member-label';
import { roleChangeGate } from './membership-gates';
import { SettingsCard, SettingsSection, SettingsTab } from './settings-layout';
import { TypedConfirmDialog } from './typed-confirm-dialog';

function toastServerFailure(
  tCommon: ReturnType<typeof useT<'common'>>['t'],
  err: unknown,
  fallback: string,
): void {
  const failure = describeServerFailure(tCommon, err, fallback);
  toast.error(
    failure.text,
    failure.detail === undefined ? undefined : { description: failure.detail },
  );
}

const ROLE_ICONS: Record<MemberRole, typeof Crown> = {
  owner: Crown,
  admin: Shield,
  member: User,
};

function useRoleLabels() {
  const { t } = useT('settings');
  return {
    owner: {
      label: t(($) => $.members.roles.owner.label),
      description: t(($) => $.members.roles.owner.description),
      icon: ROLE_ICONS.owner,
    },
    admin: {
      label: t(($) => $.members.roles.admin.label),
      description: t(($) => $.members.roles.admin.description),
      icon: ROLE_ICONS.admin,
    },
    member: {
      label: t(($) => $.members.roles.member.label),
      description: t(($) => $.members.roles.member.description),
      icon: ROLE_ICONS.member,
    },
  } as const;
}

function MemberRow({
  member,
  canManage,
  canManageOwners,
  ownerCount,
  isSelf,
  busy,
  perimeterEnabled,
  canTogglePerimeter,
  onPerimeterChange,
  onRoleChange,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  ownerCount: number;
  isSelf: boolean;
  busy: boolean;
  perimeterEnabled: boolean;
  canTogglePerimeter: boolean;
  onPerimeterChange: (granted: boolean) => void;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}) {
  const { t } = useT('settings');
  const roleConfig = useRoleLabels();
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== 'owner' || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== 'owner' || canManageOwners);
  const isLastOwner = member.role === 'owner' && ownerCount <= 1;
  const perimeterGranted = member.perimeter_access === true;
  const showPerimeterAction = perimeterEnabled && canTogglePerimeter;
  const showMenu = canEditRole || canRemove || showPerimeterAction;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size="lg" />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{member.name}</div>
        <div className="text-xs text-muted-foreground truncate">{member.email}</div>
      </div>
      {showMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" disabled={busy}>
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-auto">
            {canEditRole && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Shield className="h-3.5 w-3.5" />
                  {t(($) => $.members.change_role)}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(
                    Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]
                  ).map(([role, config]) => {
                    if (role === 'owner' && !canManageOwners) return null;
                    const Icon = config.icon;
                    const wouldDemoteLastOwner = isLastOwner && role !== 'owner';
                    return (
                      <DropdownMenuItem
                        key={role}
                        onClick={() => (wouldDemoteLastOwner ? undefined : onRoleChange(role))}
                        disabled={wouldDemoteLastOwner}
                        title={
                          wouldDemoteLastOwner
                            ? t(($) => $.members.cannot_demote_last_owner_title)
                            : undefined
                        }
                      >
                        <Icon className="h-3.5 w-3.5" />
                        <div className="flex flex-col">
                          <span>{config.label}</span>
                          <span className="text-xs text-muted-foreground font-normal">
                            {wouldDemoteLastOwner
                              ? t(($) => $.members.cannot_demote_last_owner)
                              : config.description}
                          </span>
                        </div>
                        {member.role === role && (
                          <span className="ml-auto text-xs text-muted-foreground">{'✓'}</span>
                        )}
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {showPerimeterAction && (
              <DropdownMenuItem onClick={() => onPerimeterChange(!perimeterGranted)}>
                <ShieldCheck className="h-3.5 w-3.5" />
                <div className="flex flex-col">
                  <span>
                    {perimeterGranted
                      ? t(($) => $.members.perimeter_revoke_action)
                      : t(($) => $.members.perimeter_grant_action)}
                  </span>
                  <span className="text-xs text-muted-foreground font-normal">
                    {t(($) => $.members.perimeter_description)}
                  </span>
                </div>
                {perimeterGranted && (
                  <span className="ml-auto text-xs text-muted-foreground">{'✓'}</span>
                )}
              </DropdownMenuItem>
            )}
            {(canEditRole || showPerimeterAction) && canRemove && <DropdownMenuSeparator />}
            {canRemove && (
              <DropdownMenuItem variant="destructive" onClick={onRemove}>
                <UserMinus className="h-3.5 w-3.5" />
                {t(($) => $.members.remove_action)}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {perimeterEnabled && perimeterGranted && (
        <Badge variant="outline">
          <ShieldCheck className="h-3 w-3" />
          {t(($) => $.members.perimeter_badge)}
        </Badge>
      )}
      <Badge variant="secondary">
        <RoleIcon className="h-3 w-3" />
        {rc.label}
      </Badge>
    </div>
  );
}

function InvitationRow({
  invitation,
  canManage,
  onRevoke,
  busy,
}: {
  invitation: Invitation;
  canManage: boolean;
  onRevoke: () => void;
  busy: boolean;
}) {
  const { t } = useT('settings');
  const roleConfig = useRoleLabels();
  const rc = roleConfig[invitation.role];

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <Mail className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{invitation.invitee_email}</div>
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="h-3 w-3" />
          <span>{t(($) => $.members.pending_status)}</span>
        </div>
      </div>
      {canManage && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onRevoke}
          title={t(($) => $.members.revoke_invitation_tooltip)}
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      <Badge variant="outline">{rc.label}</Badge>
    </div>
  );
}

export function MembersTab() {
  const { t } = useT('settings');
  const { t: tCommon } = useT('common');
  const roleConfig = useRoleLabels();
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: invitations = [] } = useQuery(invitationListOptions(wsId));

  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<MemberRole>('member');
  const [inviteLoading, setInviteLoading] = useState(false);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [invitationActionId, setInvitationActionId] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: 'destructive';
    onConfirm: () => Promise<void>;
  } | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);
  const [removeMember, setRemoveMember] = useState<MemberWithUser | null>(null);
  const [removing, setRemoving] = useState(false);
  const [roleChange, setRoleChange] = useState<{
    member: MemberWithUser;
    role: MemberRole;
    target: 'member' | 'workspace';
  } | null>(null);
  const [changingRole, setChangingRole] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const permissionCtx: PermissionContext = {
    userId: user?.id ?? null,
    role: currentMember?.role ?? null,
  };
  const canManageWorkspace = canManageMembers(permissionCtx).allowed;
  const isOwner = currentMember?.role === 'owner';
  const ownerCount = members.filter((m) => m.role === 'owner').length;
  const perimeterEnabled = useConfigStore(isPerimeterDeliveryProfile);

  const sendInvitation = async (email: string, role: MemberRole) => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await api.createMember(workspace.id, { email, role });
      setInviteEmail('');
      setInviteRole('member');
      qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
      toast.success(t(($) => $.members.toast_invitation_sent));
    } catch (e) {
      toastServerFailure(
        tCommon,
        e,
        t(($) => $.members.toast_invitation_failed),
      );
    } finally {
      setInviteLoading(false);
    }
  };

  const handleInviteMember = () => {
    if (!workspace) return;
    const email = inviteEmail;
    const role = inviteRole;
    if (role !== 'admin') {
      void sendInvitation(email, role);
      return;
    }
    setConfirmAction({
      title: t(($) => $.members.invite_admin_title, { email }),
      description: t(($) => $.members.invite_admin_description, { email }),
      onConfirm: () => sendInvitation(email, role),
    });
  };

  const handleRevokeInvitation = (invitation: Invitation) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.revoke_invitation_title),
      description: t(($) => $.members.revoke_invitation_description, {
        email: invitation.invitee_email,
      }),
      variant: 'destructive',
      onConfirm: async () => {
        setInvitationActionId(invitation.id);
        try {
          await api.revokeInvitation(workspace.id, invitation.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
          toast.success(t(($) => $.members.toast_invitation_revoked));
        } catch (e) {
          toastServerFailure(
            tCommon,
            e,
            t(($) => $.members.toast_invitation_revoke_failed),
          );
        } finally {
          setInvitationActionId(null);
        }
      },
    });
  };

  const applyRoleChange = async (member: MemberWithUser, role: MemberRole): Promise<boolean> => {
    if (!workspace) return false;
    setMemberActionId(member.id);
    try {
      await api.updateMember(workspace.id, member.id, { role });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(t(($) => $.members.toast_role_updated));
      return true;
    } catch (e) {
      toastServerFailure(
        tCommon,
        e,
        t(($) => $.members.toast_role_failed),
      );
      return false;
    } finally {
      setMemberActionId(null);
    }
  };

  const roleChangeCopy = (member: MemberWithUser, role: MemberRole) => {
    const name = memberLabel(member);
    const ws = workspace?.name ?? '';
    if (role === 'owner') {
      return {
        title: t(($) => $.members.role_promote_owner_title, { name, workspace: ws }),
        description: t(($) => $.members.role_promote_owner_description, {
          name,
          workspace: ws,
        }),
      };
    }
    if (member.role === 'owner') {
      return {
        title: t(($) => $.members.role_demote_owner_title, { name }),
        description:
          role === 'admin'
            ? t(($) => $.members.role_demote_owner_to_admin_description, {
                name,
                workspace: ws,
              })
            : t(($) => $.members.role_demote_owner_to_member_description, {
                name,
                workspace: ws,
              }),
      };
    }
    if (role === 'admin') {
      return {
        title: t(($) => $.members.role_promote_admin_title, { name }),
        description: t(($) => $.members.role_promote_admin_description, { name }),
      };
    }
    return {
      title: t(($) => $.members.role_demote_admin_title, { name }),
      description: t(($) => $.members.role_demote_admin_description, { name }),
    };
  };

  const requestRoleChange = (member: MemberWithUser, role: MemberRole) => {
    const gate = roleChangeGate(member.role, role);
    if (gate.kind === 'noop') return;
    if (gate.kind === 'typed') {
      setRoleChange({ member, role, target: gate.target });
      return;
    }
    const copy = roleChangeCopy(member, role);
    setConfirmAction({
      title: copy.title,
      description: copy.description,
      variant: 'destructive',
      onConfirm: async () => {
        await applyRoleChange(member, role);
      },
    });
  };

  const applyPerimeterChange = async (memberId: string, granted: boolean) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await api.updateMember(workspace.id, memberId, { perimeter_access: granted });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(
        granted
          ? t(($) => $.members.toast_perimeter_granted)
          : t(($) => $.members.toast_perimeter_revoked),
      );
    } catch (e) {
      toastServerFailure(
        tCommon,
        e,
        t(($) => $.members.toast_perimeter_failed),
      );
    } finally {
      setMemberActionId(null);
    }
  };

  const requestPerimeterChange = (member: MemberWithUser, granted: boolean) => {
    const name = memberLabel(member);
    setConfirmAction({
      title: granted
        ? t(($) => $.members.perimeter_grant_title, { name })
        : t(($) => $.members.perimeter_revoke_title, { name }),
      description: granted
        ? t(($) => $.members.perimeter_grant_description, { name })
        : t(($) => $.members.perimeter_revoke_description, { name }),
      variant: granted ? undefined : 'destructive',
      onConfirm: () => applyPerimeterChange(member.id, granted),
    });
  };

  const confirmRemoveMember = async (member: MemberWithUser) => {
    if (!workspace) return;
    setRemoving(true);
    setMemberActionId(member.id);
    try {
      await api.deleteMember(workspace.id, member.id);
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(t(($) => $.members.toast_member_removed));
      setRemoveMember(null);
    } catch (e) {
      toastServerFailure(
        tCommon,
        e,
        t(($) => $.members.toast_member_remove_failed),
      );
    } finally {
      setRemoving(false);
      setMemberActionId(null);
    }
  };

  const confirmRoleChange = async (pending: { member: MemberWithUser; role: MemberRole }) => {
    setChangingRole(true);
    const applied = await applyRoleChange(pending.member, pending.role);
    setChangingRole(false);
    if (applied) setRoleChange(null);
  };

  const runConfirmAction = async () => {
    if (!confirmAction || confirmBusy) return;
    setConfirmBusy(true);
    try {
      await confirmAction.onConfirm();
    } finally {
      setConfirmBusy(false);
      setConfirmAction(null);
    }
  };

  if (!workspace) return null;

  return (
    <SettingsTab title={t(($) => $.page.tabs.members)}>
      <SettingsSection title={t(($) => $.members.section_title, { count: members.length })}>
        {canManageWorkspace && (
          <Card>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-sm font-medium">{t(($) => $.members.invite_title)}</h3>
              </div>
              <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                <Input
                  type="email"
                  name="invite-email"
                  autoComplete="email"
                  spellCheck={false}
                  aria-label={t(($) => $.members.invite_email_placeholder)}
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder={t(($) => $.members.invite_email_placeholder)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && inviteEmail.trim()) handleInviteMember();
                  }}
                />
                <Select
                  items={(['member', 'admin'] as const).map((value) => ({
                    value,
                    label: roleConfig[value].label,
                  }))}
                  value={inviteRole}
                  onValueChange={(value) => setInviteRole(value as MemberRole)}
                >
                  <SelectTrigger size="sm">
                    <SelectValue>{() => roleConfig[inviteRole].label}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">{roleConfig.member.label}</SelectItem>
                    <SelectItem value="admin">{roleConfig.admin.label}</SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  onClick={handleInviteMember}
                  disabled={inviteLoading || !inviteEmail.trim()}
                >
                  {inviteLoading ? t(($) => $.members.inviting) : t(($) => $.members.invite_button)}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <SettingsCard>
            {members.map((m) => (
              <div key={m.id}>
                <MemberRow
                  member={m}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  ownerCount={ownerCount}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  perimeterEnabled={perimeterEnabled}
                  canTogglePerimeter={canGrantPerimeterAccess(m, permissionCtx).allowed}
                  onPerimeterChange={(granted) => requestPerimeterChange(m, granted)}
                  onRoleChange={(role) => requestRoleChange(m, role)}
                  onRemove={() => setRemoveMember(m)}
                />
              </div>
            ))}
          </SettingsCard>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.members.no_members)}</p>
        )}
      </SettingsSection>

      {invitations.length > 0 && (
        <SettingsSection title={t(($) => $.members.pending_title, { count: invitations.length })}>
          <SettingsCard>
            {invitations.map((inv) => (
              <div key={inv.id}>
                <InvitationRow
                  invitation={inv}
                  canManage={canManageWorkspace}
                  onRevoke={() => handleRevokeInvitation(inv)}
                  busy={invitationActionId === inv.id}
                />
              </div>
            ))}
          </SettingsCard>
        </SettingsSection>
      )}

      <AlertDialog
        open={!!confirmAction}
        onOpenChange={(v) => {
          if (!v && !confirmBusy) setConfirmAction(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="break-words">{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription className="break-words">
              {confirmAction?.description}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={confirmBusy}>
              {t(($) => $.members.confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === 'destructive' ? 'destructive' : 'default'}
              disabled={confirmBusy}
              onClick={() => void runConfirmAction()}
            >
              {t(($) => $.members.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {roleChange && (
        <TypedConfirmDialog
          inputId="member-role-confirm-typed"
          title={roleChangeCopy(roleChange.member, roleChange.role).title}
          description={roleChangeCopy(roleChange.member, roleChange.role).description}
          target={
            roleChange.target === 'workspace'
              ? workspace.name.trim()
              : memberLabel(roleChange.member)
          }
          unavailableNote={
            roleChange.target === 'workspace'
              ? t(($) => $.admin.typed_confirm.target_unavailable)
              : t(($) => $.members.role_change_unavailable)
          }
          confirmLabel={t(($) => $.members.confirm_action)}
          cancelLabel={t(($) => $.members.confirm_cancel)}
          loading={changingRole}
          onClose={() => setRoleChange(null)}
          onConfirm={() => void confirmRoleChange(roleChange)}
        />
      )}
      {removeMember && (
        <TypedConfirmDialog
          inputId="member-confirm-typed"
          title={t(($) => $.members.remove_member_title, {
            name: memberLabel(removeMember),
          })}
          description={t(($) => $.members.remove_member_description, {
            name: memberLabel(removeMember),
            workspace: workspace.name,
          })}
          target={memberLabel(removeMember)}
          unavailableNote={t(($) => $.members.remove_member_unavailable)}
          confirmLabel={t(($) => $.members.confirm_action)}
          cancelLabel={t(($) => $.members.confirm_cancel)}
          loading={removing}
          onClose={() => setRemoveMember(null)}
          onConfirm={() => void confirmRemoveMember(removeMember)}
        />
      )}
    </SettingsTab>
  );
}
