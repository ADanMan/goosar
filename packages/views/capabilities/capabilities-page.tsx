'use client';

import { useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Check, KeyRound, ShieldCheck, Sparkles } from 'lucide-react';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useWorkspacePaths } from '@goosar/core/paths';
import { effectiveConfigOptions } from '@goosar/core/workspace/effective-config';
import { workspaceCapabilitiesOptions } from '@goosar/core/workspace/queries';
import { cn } from '@goosar/ui/lib/utils';
import { CollectionPageHeader } from '../layout/collection-page';
import { WorkToolsSetupButton, type WorkToolsLauncherExtras } from '../onboarding/launchers';
import { useNavigation } from '../navigation';
import { WorkToolsCard } from '../onboarding/presets';
import { useT } from '../i18n';
import { CAPABILITIES_SOURCE_PARAM, CAPABILITIES_SOURCE_REMINDER } from './capabilities-source';
import { isPersonalActionable, type ServiceCredentialStatus } from './credential-status';
import { SampleTasksSection } from './sample-tasks-section';
import { useServiceCredentials } from './use-service-credentials';
import { RoleSetupSection } from './role-setup-section';

export interface CapabilitiesPageProps {
  workToolsExtras?: WorkToolsLauncherExtras;
}

export function CapabilitiesPage({ workToolsExtras }: CapabilitiesPageProps = {}) {
  const { t } = useT('workspace');
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const { data: role, isPending: rolePending } = useQuery(workspaceCapabilitiesOptions(wsId));
  const { data: effective, isPending: effectivePending } = useQuery(effectiveConfigOptions(wsId));
  const credentials = useServiceCredentials(wsId);

  const llmReady = effective?.llm?.has_api_key === true;

  const baseline = [
    {
      key: 'ask',
      title: t(($) => $.capabilities.baseline_ask_title),
      body: t(($) => $.capabilities.baseline_ask_body),
    },
    {
      key: 'docs',
      title: t(($) => $.capabilities.baseline_docs_title),
      body: t(($) => $.capabilities.baseline_docs_body),
    },
    {
      key: 'find',
      title: t(($) => $.capabilities.baseline_find_title),
      body: t(($) => $.capabilities.baseline_find_body),
    },
    {
      key: 'track',
      title: t(($) => $.capabilities.baseline_track_title),
      body: t(($) => $.capabilities.baseline_track_body),
    },
  ];

  const roleCapabilities = role?.capabilities ?? [];
  const roleKey = role?.template_key ?? '';
  const roleName = role?.role_name ?? '';
  const hasNamedRole = roleName !== '';
  const hasNoRole = !rolePending && roleKey === '';

  const services = credentials.statuses;
  const hasMissing = credentials.missing.length > 0;
  const servicesRef = useRef<HTMLElement>(null);

  const arrivedFromReminder =
    navigation.searchParams.get(CAPABILITIES_SOURCE_PARAM) === CAPABILITIES_SOURCE_REMINDER;
  useEffect(() => {
    if (!arrivedFromReminder || !hasMissing) return;
    const node = servicesRef.current;
    if (typeof node?.scrollIntoView !== 'function') return;
    node.scrollIntoView({ block: 'start' });
  }, [arrivedFromReminder, hasMissing]);

  const adminWorkVisible = llmReady || Object.keys(effective?.mcp ?? {}).length > 0;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={Sparkles}
        title={t(($) => $.capabilities.title)}
        actions={
          <WorkToolsSetupButton wsId={wsId} extras={workToolsExtras} />
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[820px] px-5 py-8 sm:px-8">
          <p className="max-w-[600px] text-sm leading-relaxed text-muted-foreground">
            {t(($) => $.capabilities.lede)}
          </p>

          {/* R-16e: what this role still needs before it can work. Renders
              nothing once every card's work is done. */}
          <RoleSetupSection />

          {/* --- The role, straight from the template row. --- */}
          {hasNamedRole ? (
            <section className="mt-9" aria-label={roleName}>
              <p className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
                {t(($) => $.capabilities.role_eyebrow)}
              </p>
              <h2 className="mt-1 font-serif text-[26px] font-medium leading-tight text-foreground">
                {roleName}
              </h2>
              {role?.role_summary ? (
                <p className="mt-1.5 max-w-[600px] text-sm leading-relaxed text-muted-foreground">
                  {role.role_summary}
                </p>
              ) : null}
              {roleCapabilities.length > 0 ? (
                <ul className="mt-5 grid gap-3 sm:grid-cols-2">
                  {roleCapabilities.map((capability) => (
                    <li key={capability.key} className="rounded-lg border bg-card p-4">
                      <p className="text-[14px] font-medium text-foreground">{capability.title}</p>
                      <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
                        {capability.body}
                      </p>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="mt-4 text-[12.5px] leading-relaxed text-muted-foreground">
                  {t(($) => $.capabilities.role_no_detail_note)}
                </p>
              )}
            </section>
          ) : hasNoRole ? (
            <p className="mt-6 rounded-lg border border-dashed bg-muted/30 p-4 text-[12.5px] leading-relaxed text-muted-foreground">
              {t(($) => $.capabilities.no_role_note)}
            </p>
          ) : null}

          {/* --- The demonstration that runs (#349). Directly under the
                  role, because it is the same data source saying the same
                  thing twice: what this role does, and what that looks like
                  when it happens. Renders nothing for a role with no tasks. --- */}
          {credentials.isLoading ? null : (
            <SampleTasksSection tasks={role?.sample_tasks ?? []} credentials={credentials} />
          )}

          {/* --- The baseline: what every role gets. --- */}
          <section className="mt-10" aria-label={t(($) => $.capabilities.baseline_heading)}>
            <h2 className="text-[15px] font-medium text-foreground">
              {t(($) => $.capabilities.baseline_heading)}
            </h2>
            <ul className="mt-4 grid gap-3 sm:grid-cols-2">
              {baseline.map((item) => (
                <li key={item.key} className="rounded-lg border bg-card p-4">
                  <p className="text-[14px] font-medium text-foreground">{item.title}</p>
                  <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
                    {item.body}
                  </p>
                </li>
              ))}
            </ul>
          </section>

          {/* --- The boundary, stated before any of it is shown, so the
                  service list below reads as information rather than as a
                  list of chores. --- */}
          <section className="mt-10" aria-label={t(($) => $.capabilities.boundary_heading)}>
            <h2 className="text-[15px] font-medium text-foreground">
              {t(($) => $.capabilities.boundary_heading)}
            </h2>
            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border bg-card p-4">
                <p className="flex items-center gap-2 text-[14px] font-medium text-foreground">
                  <ShieldCheck className="h-4 w-4 text-muted-foreground" aria-hidden />
                  {t(($) => $.capabilities.boundary_admin_title)}
                </p>
                <p className="mt-1.5 text-[12.5px] leading-[1.55] text-muted-foreground">
                  {adminWorkVisible
                    ? t(($) => $.capabilities.boundary_admin_body)
                    : t(($) => $.capabilities.boundary_admin_body_unconfirmed)}
                </p>
                {!effectivePending && llmReady ? (
                  <p className="mt-3 flex items-center gap-1.5 text-[12px] text-success">
                    <Check className="h-3.5 w-3.5" aria-hidden />
                    {t(($) => $.capabilities.llm_ready)}
                  </p>
                ) : null}
              </div>
              <div className="rounded-lg border bg-card p-4">
                <p className="flex items-center gap-2 text-[14px] font-medium text-foreground">
                  <KeyRound className="h-4 w-4 text-muted-foreground" aria-hidden />
                  {t(($) => $.capabilities.boundary_you_title)}
                </p>
                <p className="mt-1.5 text-[12.5px] leading-[1.55] text-muted-foreground">
                  {t(($) => $.capabilities.boundary_you_body)}
                </p>
              </div>
            </div>
          </section>

          {/* --- The services themselves. READ-ONLY on purpose: credentials
                  are entered in exactly one place (the agent's MCP
                  configuration), and turning this page into a second input
                  surface would make four places where the same value can be
                  typed. The card links there instead.

                  No services is the ordinary answer for most workspaces, and
                  it renders as nothing: a heading reading "your work
                  services" over an explanation of why there are none is
                  still a paragraph about somebody else's corporate stack. --- */}
          {credentials.isLoading ? null : services.length === 0 ? null : (
            <section
              ref={servicesRef}
              className="mt-10 scroll-mt-6"
              aria-label={t(($) => $.capabilities.services_heading)}
            >
              <h2 className="text-[15px] font-medium text-foreground">
                {t(($) => $.capabilities.services_heading)}
              </h2>
              <div className="mt-4 flex flex-col gap-4">
                {services.map((status) => (
                  <WorkToolsCard
                    key={status.preset}
                    preset={status.preset}
                    badge={<ServiceBadges status={status} />}
                    footer={
                      credentials.helper &&
                      (status.personal === 'missing' || status.personal === 'filled') &&
                      status.admin !== 'off' ? (
                        <button
                          type="button"
                          className="text-[12.5px] font-medium text-primary underline-offset-4 hover:underline"
                          onClick={() =>
                            navigation.push(
                              `${paths.agentDetail(credentials.helper!.id)}?view=mcp_config`,
                            )
                          }
                        >
                          {t(($) => $.capabilities.open_setup)}
                        </button>
                      ) : null
                    }
                  />
                ))}
              </div>
              <p className="mt-4 text-[12px] leading-relaxed text-muted-foreground/80">
                {t(($) => $.capabilities.saved_not_verified)}
              </p>
            </section>
          )}
        </div>
      </div>
    </div>
  );
}

CapabilitiesPage.displayName = 'CapabilitiesPage';

function ServiceBadges({ status }: { status: ServiceCredentialStatus }) {
  const { t } = useT('workspace');

  const adminLabel =
    status.admin === 'provided'
      ? t(($) => $.capabilities.badge_admin_provided)
      : status.admin === 'off'
        ? t(($) => $.capabilities.badge_admin_off)
        : null;

  const personalLabel =
    isPersonalActionable(status)
      ? t(($) => $.capabilities.badge_you_missing)
      : status.personal === 'filled'
        ? t(($) => $.capabilities.badge_you_filled)
        : status.personal === 'not_applicable'
          ? t(($) => $.capabilities.badge_you_none)
          : null;

  if (adminLabel === null && personalLabel === null) return null;

  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {adminLabel !== null ? (
        <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
          {adminLabel}
        </span>
      ) : null}
      {personalLabel !== null ? (
        <span
          className={cn(
            'rounded-full px-2 py-0.5 text-[11px] font-medium',
            isPersonalActionable(status)
              ? 'bg-warning/10 text-warning'
              : status.personal === 'filled'
                ? 'bg-success/10 text-success'
                : 'bg-muted text-muted-foreground',
          )}
        >
          {personalLabel}
        </span>
      ) : null}
    </span>
  );
}
