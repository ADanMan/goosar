'use client';

import { useQuery } from '@tanstack/react-query';
import { MessagesSquare } from 'lucide-react';
import type { Agent } from '@goosar/core/types';
import { useAuthStore } from '@goosar/core/auth';
import { useWorkspaceId } from '@goosar/core/hooks';
import { slackInstallationsOptions } from '@goosar/core/slack';
import { memberListOptions } from '@goosar/core/workspace/queries';
import { SlackAgentBindButton } from '../../../settings/components/slack-tab';
import { useT } from '../../../i18n';

export function IntegrationsTab({ agent }: { agent: Agent }) {
  const { t } = useT('agents');
  const { t: ts } = useT('settings');
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  const { data: slackListing } = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isWorkspaceAdmin = currentMember?.role === 'owner' || currentMember?.role === 'admin';
  const canManageSlack = isWorkspaceAdmin;

  const slackConfigured = slackListing?.configured === true;
  const slackInstallSupported = slackListing?.install_supported === true;
  const slackHasActiveInstall =
    slackListing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === 'active',
    ) ?? false;

  if (!canManageSlack) {
    return (
      <div className="space-y-6">
        <p className="text-xs text-muted-foreground">{t(($) => $.tab_body.integrations.intro)}</p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.integrations.members_note)}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <p className="text-xs text-muted-foreground">{t(($) => $.tab_body.integrations.intro)}</p>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <MessagesSquare className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-sm font-medium">{ts(($) => $.slack.section_title)}</h3>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {ts(($) => $.slack.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageSlack ? (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !slackConfigured ? (
            <p className="text-xs text-muted-foreground">{ts(($) => $.slack.not_enabled_title)}</p>
          ) : !slackInstallSupported && !slackHasActiveInstall ? (
            <div className="space-y-1">
              <p className="text-xs font-medium">{ts(($) => $.slack.preview_title)}</p>
              <p className="text-xs text-muted-foreground">
                {ts(($) => $.slack.preview_description)}
              </p>
            </div>
          ) : (
            <SlackAgentBindButton agentId={agent.id} agentName={agent.name} />
          )}
        </div>
      </section>
    </div>
  );
}
