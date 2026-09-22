'use client';

import { useQuery } from '@tanstack/react-query';
import {
  deploymentAdminPendingOptions,
  deploymentAdminsOptions,
} from '@goosar/core/deployment/admin';
import { useT } from '../../i18n';
import { SettingsTab } from './settings-layout';
import { DeploymentAdminsSection } from './deployment-admins-section';
import { DeploymentAuditSection } from './deployment-audit-section';
import { DeploymentFleetSection } from './deployment-fleet-section';
import { DeploymentMcpSection } from './deployment-mcp-section';
import { DeploymentPolicySection } from './deployment-policy-section';
import { DeploymentSessionsSection } from './deployment-sessions-section';
import { DeploymentWorkspacesSection } from './deployment-workspaces-section';

export function DeploymentTab() {
  const { t } = useT('settings');
  const { data: admins } = useQuery(deploymentAdminsOptions());
  const { data: pending } = useQuery({
    ...deploymentAdminPendingOptions(),
    enabled: Array.isArray(admins),
  });

  if (!Array.isArray(admins)) return null;

  return (
    <SettingsTab
      title={t(($) => $.deployment.tab_title)}
      description={t(($) => $.deployment.description)}
    >
      <DeploymentAdminsSection admins={admins} pending={pending ?? []} />
      <DeploymentPolicySection />
      {/* Session policy (#391) sits with the rest of the policy layer because
          it IS the policy layer — the same document, the same PUT. */}
      <DeploymentSessionsSection />
      {/*
        The library sits between the policy layer and the workspace directory
        deliberately: it is deployment-level composition like the policy, but
        unlike the policy it grants nothing on its own — each workspace in the
        directory below decides for itself whether to take a record.
      */}
      <DeploymentMcpSection />
      <DeploymentWorkspacesSection />
      {/*
        The fleet sits after the directory because it reads the same
        deployment from the other end: the directory lists the workspaces an
        admin manages, the fleet lists the machines those workspaces actually
        run on (#398).
      */}
      <DeploymentFleetSection />
      {/*
        Last: the journal is what the sections above LEAVE BEHIND — read it
        after acting, not before.
      */}
      <DeploymentAuditSection />
    </SettingsTab>
  );
}
