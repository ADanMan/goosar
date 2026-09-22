'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, ChevronRight } from 'lucide-react';
import { ComposioTab } from './composio-tab';
import { SlackTab } from './slack-tab';
import { VCSTab } from './vcs-tab';
import { GitHubTab } from './github-tab';
import { GitHubMark } from './github-mark';
import { ApiError } from '@goosar/core/api';
import { composioConnectionsOptions, composioToolkitsOptions } from '@goosar/core/composio';
import { useConfigStore, useFeatureEnabled } from '@goosar/core/config';
import { COMPOSIO_MCP_APPS_FLAG } from '@goosar/core/feature-flags';
import { githubInstallationsOptions } from '@goosar/core/github';
import { useWorkspaceId } from '@goosar/core/hooks';
import { slackInstallationsOptions } from '@goosar/core/slack';
import { vcsConnectionsOptions } from '@goosar/core/vcs';
import { Button } from '@goosar/ui/components/ui/button';
import { useNavigation } from '../../navigation';
import { WorkToolsSetupButton } from '../../onboarding/launchers';
import { useT, useUiLocale } from '../../i18n';
import { SettingsCard, SettingsSection, SettingsTab } from './settings-layout';
import {
  installationIntegrationStatus,
  type IntegrationConfigurableBy,
  type IntegrationStatus,
} from './integration-status';

type IntegrationId = 'github' | 'slack' | 'composio' | 'vcs';

interface IntegrationEntry {
  id: IntegrationId;
  name: string;
  status: IntegrationStatus;
  configurableBy: IntegrationConfigurableBy;
  icon?: ReactNode;
  panel: ReactNode;
}

export function IntegrationsTab() {
  const { t } = useT('settings');
  const locale = useUiLocale();
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const callbackQuery = navigation.searchParams.toString();
  const [open, setOpen] = useState<IntegrationId | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(callbackQuery);
    if (params.get('connected') !== null || params.get('error') === 'composio_connect_failed') {
      setOpen('composio');
      return;
    }
    if (params.get('github_connected') === null && params.get('github_error') === null) {
      return;
    }
    setOpen('github');
    params.delete('github_connected');
    params.delete('github_error');
    const qs = params.toString();
    navigation.replace(qs ? `${navigation.pathname}?${qs}` : navigation.pathname);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [callbackQuery]);

  const composioEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const composioToolkits = useQuery({
    ...composioToolkitsOptions(),
    enabled: composioEnabled,
  });
  const composioUnconfigured =
    composioToolkits.error instanceof ApiError && composioToolkits.error.status === 503;
  const composioConnections = useQuery({
    ...composioConnectionsOptions(),
    enabled: composioEnabled && !composioUnconfigured,
  });

  const vcsAvailable = useConfigStore((s) => s.vcsIntegrationAvailable);

  const github = useQuery(githubInstallationsOptions(wsId));
  const slack = useQuery({ ...slackInstallationsOptions(wsId), enabled: !!wsId });
  const vcs = useQuery({ ...vcsConnectionsOptions(wsId), enabled: vcsAvailable });

  const entries: IntegrationEntry[] = [];

  entries.push({
    id: 'github',
    name: 'GitHub',
    icon: <GitHubMark className="size-4" />,
    status: installationIntegrationStatus({
      configured: github.data?.configured === true,
      connectedCount: github.data?.installations?.length ?? 0,
      loaded: github.isSuccess,
    }),
    configurableBy: 'workspace_admin',
    panel: <GitHubTab />,
  });

  entries.push({
    id: 'slack',
    name: t(($) => $.slack.section_title),
    status: installationIntegrationStatus({
      configured: slack.data?.configured === true,
      connectedCount: slack.data?.installations?.length ?? 0,
      loaded: slack.isSuccess,
    }),
    configurableBy: 'workspace_admin',
    panel: <SlackTab />,
  });

  if (composioEnabled && !composioUnconfigured) {
    entries.push({
      id: 'composio',
      name: t(($) => $.composio.section_title),
      status: installationIntegrationStatus({
        configured: true,
        connectedCount: (composioConnections.data ?? []).filter((c) => c.status === 'active')
          .length,
        loaded: composioConnections.isSuccess,
      }),
      configurableBy: 'member',
      panel: <ComposioTab />,
    });
  }

  if (vcsAvailable) {
    entries.push({
      id: 'vcs',
      name: t(($) => $.vcs.section_title),
      status: installationIntegrationStatus({
        configured: vcs.data?.configured === true,
        connectedCount: vcs.data?.connections?.length ?? 0,
        loaded: vcs.isSuccess,
      }),
      configurableBy: 'workspace_admin',
      panel: <VCSTab />,
    });
  }

  const active = entries.find((entry) => entry.id === open) ?? null;

  if (active) {
    return (
      <SettingsTab title={active.name}>
        <div>
          <Button variant="ghost" size="sm" onClick={() => setOpen(null)}>
            <ArrowLeft className="size-3.5" />
            {t(($) => $.integrations.back_to_list)}
          </Button>
        </div>
        {active.panel}
      </SettingsTab>
    );
  }

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.integrations)}
      description={t(($) => $.integrations.list_description)}
    >
      <SettingsCard>
        {entries.map((entry) => (
          <IntegrationRow
            key={entry.id}
            entry={entry}
            locale={locale}
            onConfigure={() => setOpen(entry.id)}
          />
        ))}
      </SettingsCard>

      {/* R-16b: the personal MCP credentials the onboarding work-tools step
          collects are not one integration's — they belong to the member, not
          to the workspace — so they get their own entry point rather than a
          row with a workspace-level status. */}
      <SettingsSection
        title={t(($) => $.integrations.work_tools_title)}
        description={t(($) => $.integrations.work_tools_description)}
      >
        <WorkToolsSetupButton wsId={wsId} />
      </SettingsSection>
    </SettingsTab>
  );
}

function IntegrationRow({
  entry,
  locale,
  onConfigure,
}: {
  entry: IntegrationEntry;
  locale: string;
  onConfigure: () => void;
}) {
  const { t } = useT('settings');

  const configurer: IntegrationConfigurableBy =
    entry.status.kind === 'not_configured' ? 'deployment_admin' : entry.configurableBy;

  return (
    <div
      data-testid={`integration-row-${entry.id}`}
      className="flex min-h-16 flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between sm:gap-8"
    >
      <div className="flex min-w-0 flex-1 items-start gap-3">
        {entry.icon ? (
          <span className="mt-0.5 shrink-0 text-muted-foreground">{entry.icon}</span>
        ) : null}
        <div className="min-w-0">
          <div className="text-sm font-medium">{entry.name}</div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs leading-5 text-muted-foreground">
            <StatusLabel status={entry.status} locale={locale} />
            <span aria-hidden>·</span>
            <span>{t(($) => $.integrations.configurable_by[configurer])}</span>
          </div>
        </div>
      </div>
      <Button variant="outline" size="sm" onClick={onConfigure} className="shrink-0">
        {t(($) => $.integrations.configure)}
        <ChevronRight className="size-3.5" />
      </Button>
    </div>
  );
}

function StatusLabel({ status, locale }: { status: IntegrationStatus; locale: string }) {
  const { t } = useT('settings');

  switch (status.kind) {
    case 'connected':
      return <span className="text-success">{t(($) => $.integrations.status.connected)}</span>;
    case 'no_credentials':
      return <span>{t(($) => $.integrations.status.no_credentials)}</span>;
    case 'not_configured':
      return <span>{t(($) => $.integrations.status.not_configured)}</span>;
    case 'health_error': {
      const at = status.at ? new Date(status.at) : null;
      const when =
        at && !Number.isNaN(at.getTime())
          ? at.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })
          : null;
      return (
        <span className="text-destructive">
          {when
            ? t(($) => $.integrations.status.health_error_at, { when })
            : t(($) => $.integrations.status.health_error)}
        </span>
      );
    }
    default:
      return <span>{t(($) => $.integrations.status.unknown)}</span>;
  }
}
