'use client';

import { useCallback, useEffect, useState } from 'react';
import { KeyRound } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import { paths, useCurrentWorkspace, useWorkspaceSlug } from '@goosar/core/paths';
import { Button } from '@goosar/ui/components/ui/button';
import { useNavigation } from '../navigation';
import { useT } from '../i18n';
import { workToolsPresetText } from '../onboarding/presets';
import { CAPABILITIES_SOURCE_PARAM, CAPABILITIES_SOURCE_REMINDER } from './capabilities-source';
import {
  shouldShowCredentialBanner,
  useCredentialBannerDismiss,
} from './credential-banner-dismiss';
import { useServiceCredentials } from './use-service-credentials';

export function MissingCredentialsBanner() {
  const { t } = useT('workspace');
  const { t: tOnboarding } = useT('onboarding');
  const workspace = useCurrentWorkspace();
  const workspaceSlug = useWorkspaceSlug();
  const navigation = useNavigation();
  const userId = useAuthStore((state) => state.user?.id ?? null);

  const credentials = useServiceCredentials(workspace?.id ?? '');

  const {
    state: reminder,
    recordAppearance,
    dismiss: spendBudget,
  } = useCredentialBannerDismiss(userId, workspace?.id ?? null);
  const [closedSignature, setClosedSignature] = useState<string | null>(null);

  const signature = credentials.signature;

  const dismiss = useCallback(
    (signature: string) => {
      setClosedSignature(signature);
      spendBudget(signature);
    },
    [spendBudget],
  );

  const [shownSignature, setShownSignature] = useState<string | null>(null);

  const visible =
    !!workspace &&
    !!workspaceSlug &&
    !credentials.isLoading &&
    credentials.missing.length > 0 &&
    closedSignature !== signature &&
    (shownSignature === signature || shouldShowCredentialBanner(reminder, signature));

  useEffect(() => {
    if (!visible) return;
    if (shownSignature === signature) return;
    setShownSignature(signature);
    recordAppearance(signature);
  }, [visible, shownSignature, signature, recordAppearance]);

  if (!visible) return null;

  const services = credentials.missing
    .map((preset) => workToolsPresetText(tOnboarding, preset).title)
    .join(', ');

  return (
    <div
      role="status"
      className="pointer-events-none fixed inset-x-0 bottom-0 z-40 flex justify-center px-4 pb-4"
    >
      <div className="pointer-events-auto flex w-full max-w-[560px] items-start gap-3 rounded-lg border bg-card p-4 shadow-lg">
        <KeyRound className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-medium text-foreground">
            {t(($) => $.credentials_banner.title)}
          </p>
          <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
            {t(($) => $.credentials_banner.body, { services })}
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              onClick={() => {
                setClosedSignature(signature);
                navigation.push(
                  `${paths.workspace(workspaceSlug).capabilities()}?${CAPABILITIES_SOURCE_PARAM}=${CAPABILITIES_SOURCE_REMINDER}`,
                );
              }}
            >
              {t(($) => $.credentials_banner.action)}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => dismiss(signature)}>
              {t(($) => $.credentials_banner.dismiss)}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

MissingCredentialsBanner.displayName = 'MissingCredentialsBanner';
