'use client';

import { useState, type ReactNode } from 'react';
import {
  Building2,
  ChevronDown,
  ChevronUp,
  ClipboardList,
  Globe,
  Mail,
  Network,
} from 'lucide-react';
import {
  configStore,
  deploymentHostLabel,
  isPerimeterDeliveryProfile,
  useConfigStore,
  type DeploymentHosts,
} from '@goosar/core/config';
import { useT } from '../../i18n';
import type { HelperMcpPresetName } from './mcp-presets';

const PRESET_ICONS: Record<HelperMcpPresetName, ReactNode> = {
  atlassian: <ClipboardList className="h-4 w-4" aria-hidden />,
  outlook: <Mail className="h-4 w-4" aria-hidden />,
  bitrix24: <Building2 className="h-4 w-4" aria-hidden />,
  fetch: <Globe className="h-4 w-4" aria-hidden />,
  'mcp-gateway': <Network className="h-4 w-4" aria-hidden />,
};

interface PresetText {
  title: string;
  description: string;
  guide: string;
  rights: string;
  verify: string;
  adminNote: string;
  adminNoteSelfInstall?: string;
}

type OnboardingTranslate = ReturnType<typeof useT<'onboarding'>>['t'];

export function workToolsPresetText(
  t: OnboardingTranslate,
  preset: HelperMcpPresetName,
  hosts: DeploymentHosts = configStore.getState().deploymentHosts,
): PresetText {
  const jiraHost = deploymentHostLabel(hosts.jiraUrl);
  const confluenceHost = deploymentHostLabel(hosts.confluenceUrl);
  const atlassianHostsKnown = jiraHost !== '' && confluenceHost !== '';
  switch (preset) {
    case 'atlassian':
      return {
        title: t(($) => $.step_work_tools.presets.atlassian.title),
        description: t(($) => $.step_work_tools.presets.atlassian.description),
        guide: atlassianHostsKnown
          ? t(($) => $.step_work_tools.presets.atlassian.guide, {
              jiraHost,
              confluenceHost,
            })
          : t(($) => $.step_work_tools.presets.atlassian.guide_unknown_hosts),
        rights: t(($) => $.step_work_tools.presets.atlassian.rights),
        verify: t(($) => $.step_work_tools.presets.atlassian.verify),
        adminNote: t(($) => $.step_work_tools.presets.atlassian.admin_note),
      };
    case 'outlook':
      return {
        title: t(($) => $.step_work_tools.presets.outlook.title),
        description: t(($) => $.step_work_tools.presets.outlook.description),
        guide: t(($) => $.step_work_tools.presets.outlook.guide),
        rights: t(($) => $.step_work_tools.presets.outlook.rights),
        verify: t(($) => $.step_work_tools.presets.outlook.verify),
        adminNote: t(($) => $.step_work_tools.presets.outlook.admin_note),
      };
    case 'bitrix24':
      return {
        title: t(($) => $.step_work_tools.presets.bitrix24.title),
        description: t(($) => $.step_work_tools.presets.bitrix24.description),
        guide: t(($) => $.step_work_tools.presets.bitrix24.guide),
        rights: t(($) => $.step_work_tools.presets.bitrix24.rights),
        verify: t(($) => $.step_work_tools.presets.bitrix24.verify),
        adminNote: t(($) => $.step_work_tools.presets.bitrix24.admin_note),
      };
    case 'fetch':
      return {
        title: t(($) => $.step_work_tools.presets.fetch.title),
        description: t(($) => $.step_work_tools.presets.fetch.description),
        guide: t(($) => $.step_work_tools.presets.fetch.guide),
        rights: t(($) => $.step_work_tools.presets.fetch.rights),
        verify: t(($) => $.step_work_tools.presets.fetch.verify),
        adminNote: t(($) => $.step_work_tools.presets.fetch.admin_note),
      };
    case 'mcp-gateway':
      return {
        title: t(($) => $.step_work_tools.presets.mcp_gateway.title),
        description: t(($) => $.step_work_tools.presets.mcp_gateway.description),
        guide: t(($) => $.step_work_tools.presets.mcp_gateway.guide),
        rights: t(($) => $.step_work_tools.presets.mcp_gateway.rights),
        verify: t(($) => $.step_work_tools.presets.mcp_gateway.verify),
        adminNote: t(($) => $.step_work_tools.presets.mcp_gateway.admin_note),
        adminNoteSelfInstall: t(
          ($) => $.step_work_tools.presets.mcp_gateway.admin_note_self_install,
        ),
      };
  }
}

export function useWorkToolsPresetText(preset: HelperMcpPresetName): PresetText {
  const { t } = useT('onboarding');
  const hosts = useConfigStore((s) => s.deploymentHosts);
  return workToolsPresetText(t, preset, hosts);
}

export function WorkToolsCard({
  preset,
  badge,
  children,
  footer,
}: {
  preset: HelperMcpPresetName;
  badge?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
}) {
  const { t } = useT('onboarding');
  const text = useWorkToolsPresetText(preset);
  const [guideOpen, setGuideOpen] = useState(false);
  const selfInstallVisible = !useConfigStore(isPerimeterDeliveryProfile);

  return (
    <section className="rounded-lg border bg-card p-5" aria-label={text.title}>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="text-muted-foreground">{PRESET_ICONS[preset]}</span>
        <h2 className="text-[14.5px] font-medium text-foreground">{text.title}</h2>
        {badge}
      </div>
      <p className="mt-1.5 text-[12.5px] leading-[1.55] text-muted-foreground">
        {text.description}
      </p>

      {children ? <div className="mt-4 flex flex-col gap-4">{children}</div> : null}

      <button
        type="button"
        onClick={() => setGuideOpen((open) => !open)}
        aria-expanded={guideOpen}
        className="mt-4 flex items-center gap-1.5 text-[12.5px] font-medium text-muted-foreground transition-colors hover:text-foreground"
      >
        {guideOpen ? (
          <ChevronUp className="h-3.5 w-3.5" aria-hidden />
        ) : (
          <ChevronDown className="h-3.5 w-3.5" aria-hidden />
        )}
        {guideOpen
          ? t(($) => $.step_work_tools.guide_hide)
          : t(($) => $.step_work_tools.guide_show)}
      </button>

      {guideOpen && (
        <div className="mt-3 flex flex-col gap-3 rounded-md bg-muted/40 p-3.5">
          <GuideSection
            heading={t(($) => $.step_work_tools.guide_steps_heading)}
            body={text.guide}
          />
          <GuideSection
            heading={t(($) => $.step_work_tools.guide_rights_heading)}
            body={text.rights}
          />
          <GuideSection
            heading={t(($) => $.step_work_tools.guide_verify_heading)}
            body={text.verify}
          />
          <GuideSection
            heading={t(($) => $.step_work_tools.guide_admin_heading)}
            body={
              selfInstallVisible && text.adminNoteSelfInstall
                ? `${text.adminNote}\n${text.adminNoteSelfInstall}`
                : text.adminNote
            }
          />
        </div>
      )}

      {footer ? <div className="mt-4">{footer}</div> : null}
    </section>
  );
}

WorkToolsCard.displayName = 'WorkToolsCard';

function GuideSection({ heading, body }: { heading: string; body: string }) {
  return (
    <div>
      <p className="text-[11px] font-medium uppercase tracking-[0.07em] text-muted-foreground/70">
        {heading}
      </p>
      <p className="mt-1 whitespace-pre-line text-[12.5px] leading-[1.6] text-muted-foreground">
        {body}
      </p>
    </div>
  );
}
