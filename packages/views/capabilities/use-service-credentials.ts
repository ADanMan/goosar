'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { isPerimeterDeliveryProfile, useConfigStore } from '@goosar/core/config';
import { useCurrentMember } from '@goosar/core/permissions';
import { effectiveConfigOptions } from '@goosar/core/workspace/effective-config';
import { agentListOptions } from '@goosar/core/workspace/queries';
import type { Agent } from '@goosar/core/types';
import { buildHelperMcpConfig } from '../onboarding/presets';
import { pickOwnWorkspaceHelper } from '../workspace/helper-setup';
import {
  missingCredentialsSignature,
  missingPersonalCredentials,
  serviceCredentialStatuses,
  type ServiceCredentialStatus,
} from './credential-status';
import type { HelperMcpPresetName } from '../onboarding/presets';

export interface ServiceCredentialsView {
  statuses: ServiceCredentialStatus[];
  missing: HelperMcpPresetName[];
  signature: string;
  isLoading: boolean;
  helper: Agent | null;
}

export function useServiceCredentials(
  wsId: string,
  installedPackageNames?: readonly string[],
): ServiceCredentialsView {
  const perimeterProfile = useConfigStore(isPerimeterDeliveryProfile);
  const enabled = wsId !== '';
  const { member, isLoading: memberLoading } = useCurrentMember(wsId);
  const userId = useAuthStore((state) => state.user?.id ?? null);

  const { data: agents, isPending: agentsPending } = useQuery({
    ...agentListOptions(wsId),
    enabled,
  });
  const helper = useMemo(() => pickOwnWorkspaceHelper(agents ?? [], userId), [agents, userId]);

  const { data: effective, isPending: effectivePending } = useQuery({
    ...effectiveConfigOptions(wsId),
    enabled,
  });

  const catalogAvailable = !perimeterProfile || member?.perimeter_access === true;

  const isLoading = !enabled || memberLoading || agentsPending || effectivePending;

  const statuses = serviceCredentialStatuses({
    helperMcpConfig: helper?.mcp_config ?? null,
    helperPresent: !!helper,
    helperConfigRedacted: helper?.mcp_config_redacted === true,
    effectiveMcp: effective?.mcp,
    installedPackageNames,
    pristine: buildHelperMcpConfig().mcpServers,
    catalogAvailable,
    corporateDeployment: perimeterProfile,
  });

  const missing = isLoading ? [] : missingPersonalCredentials(statuses);

  return {
    statuses,
    missing,
    signature: missingCredentialsSignature(missing),
    isLoading,
    helper: helper ?? null,
  };
}
