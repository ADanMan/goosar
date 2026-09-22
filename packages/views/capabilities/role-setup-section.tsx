'use client';

import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Monitor, Zap } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@goosar/core/api';
import { autopilotKeys, autopilotListOptions } from '@goosar/core/autopilots';
import { useWorkspaceId } from '@goosar/core/hooks';
import { runtimeListOptions } from '@goosar/core/runtimes/queries';
import { Button } from '@goosar/ui/components/ui/button';
import { RuntimeConnectButton } from '../onboarding/launchers';
import { useT } from '../i18n';
import { roleSetupCards, type RoleSetupCard } from './role-setup-cards';

export function RoleSetupSection() {
  const { t } = useT('workspace');
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [enabling, setEnabling] = useState(false);

  const autopilots = useQuery({
    ...autopilotListOptions(wsId),
    enabled: !!wsId,
  });
  const runtimes = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });

  const paused = (autopilots.data ?? []).filter(
    (a) => a.status === 'paused' && a.is_template === true,
  );

  const ready = !!wsId && autopilots.isSuccess && runtimes.isSuccess;

  const cards = roleSetupCards({
    ready,
    pausedAutopilotCount: paused.length,
    hasPublicRuntime: (runtimes.data ?? []).some((rt) => rt.visibility === 'public'),
  });

  if (cards.length === 0) return null;

  const enableAutopilots = async () => {
    if (enabling) return;
    setEnabling(true);
    try {
      const results = await Promise.allSettled(
        paused.map((a) => api.updateAutopilot(a.id, { status: 'active' })),
      );
      const failed = results.filter((r) => r.status === 'rejected').length;
      if (failed > 0) {
        toast.error(t(($) => $.role_setup.autopilots_partial, { count: failed }));
      } else {
        toast.success(t(($) => $.role_setup.autopilots_enabled));
      }
    } finally {
      setEnabling(false);
      await queryClient.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    }
  };

  return (
    <section className="mt-9" aria-label={t(($) => $.role_setup.title)}>
      <h2 className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.role_setup.title)}
      </h2>
      <div className="mt-3 flex flex-col gap-3">
        {cards.map((card) => (
          <RoleCard
            key={card.kind}
            card={card}
            enabling={enabling}
            onEnableAutopilots={enableAutopilots}
            wsId={wsId}
          />
        ))}
      </div>
    </section>
  );
}

function RoleCard({
  card,
  enabling,
  onEnableAutopilots,
  wsId,
}: {
  card: RoleSetupCard;
  enabling: boolean;
  onEnableAutopilots: () => void;
  wsId: string;
}) {
  const { t } = useT('workspace');

  if (card.kind === 'autopilots') {
    return (
      <Row
        testId="role-card-autopilots"
        icon={<Zap className="size-4" />}
        title={t(($) => $.role_setup.autopilots_title, { count: card.count })}
        body={t(($) => $.role_setup.autopilots_body)}
        action={
          <Button size="sm" disabled={enabling} onClick={onEnableAutopilots}>
            {enabling
              ? t(($) => $.role_setup.autopilots_enabling)
              : t(($) => $.role_setup.autopilots_action)}
          </Button>
        }
      />
    );
  }

  return (
    <Row
      testId="role-card-runtime"
      icon={<Monitor className="size-4" />}
      title={t(($) => $.role_setup.runtime_title)}
      body={t(($) => $.role_setup.runtime_body)}
      action={<RuntimeConnectButton wsId={wsId} variant="default" />}
    />
  );
}

function Row({
  testId,
  icon,
  title,
  body,
  action,
}: {
  testId: string;
  icon: React.ReactNode;
  title: string;
  body: string;
  action: React.ReactNode;
}) {
  return (
    <div
      data-testid={testId}
      className="flex flex-col gap-3 rounded-lg border bg-card p-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6"
    >
      <div className="flex min-w-0 items-start gap-3">
        <span aria-hidden className="mt-0.5 shrink-0 text-muted-foreground">
          {icon}
        </span>
        <div className="min-w-0">
          <div className="text-[14.5px] font-medium text-foreground">{title}</div>
          <p className="mt-1 text-[13px] leading-[1.55] text-muted-foreground">{body}</p>
        </div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}
