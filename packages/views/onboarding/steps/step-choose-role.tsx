'use client';

import { useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Users } from 'lucide-react';
import { Button } from '@goosar/ui/components/ui/button';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { cn } from '@goosar/ui/lib/utils';
import { useConfigStore } from '@goosar/core/config';
import { joinTargetListOptions, workspaceListOptions } from '@goosar/core/workspace/queries';
import { useJoinWorkspace } from '@goosar/core/workspace/mutations';
import type { Workspace } from '@goosar/core/types';
import { DragStrip } from '@goosar/views/platform';
import { StepHeader } from '../components/step-header';
import { useT } from '../../i18n';

export function StepChooseRole({
  onJoined,
  onBack,
  onCreateInstead,
}: {
  onJoined: (workspace: Workspace) => void | Promise<void>;
  onBack?: () => void;
  onCreateInstead?: () => void;
}) {
  const { t } = useT('onboarding');
  const { t: tWorkspace } = useT('workspace');
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);
  const queryClient = useQueryClient();
  const { data: targets = [], isLoading } = useQuery(joinTargetListOptions());
  const join = useJoinWorkspace();
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);

  const onJoin = (id: string) => {
    setPendingId(id);
    setFailed(false);
    join.mutate(id, {
      onSuccess: async (result) => {
        try {
          const list = await queryClient.fetchQuery(workspaceListOptions());
          const joined =
            list.find((ws) => ws.id === result.id) ??
            list.find((ws) => ws.slug === result.slug) ??
            null;
          if (!joined) {
            setPendingId(null);
            setFailed(true);
            return;
          }
          setPendingId(null);
          await onJoined(joined);
        } catch {
          setPendingId(null);
          setFailed(true);
        }
      },
      onError: () => {
        setPendingId(null);
        setFailed(true);
      },
    });
  };

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col">
      <DragStrip />
      <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            disabled={pendingId !== null}
            className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            {t(($) => $.common.back)}
          </button>
        ) : (
          <span aria-hidden className="w-0" />
        )}
        <div className="flex-1">
          <StepHeader currentStep="workspace" />
        </div>
      </header>

      <main ref={mainRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
          <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
            {t(($) => $.step_choose_role.eyebrow)}
          </div>
          <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
            {t(($) => $.step_choose_role.headline)}
          </h1>
          <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
            {t(($) => $.step_choose_role.lede)}
          </p>

          <div className="mt-10">
            {isLoading ? (
              <p className="text-sm text-muted-foreground">
                {tWorkspace(($) => $.join_page.loading)}
              </p>
            ) : targets.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t(($) => $.step_choose_role.empty)}</p>
            ) : (
              <ul className="flex flex-col gap-3">
                {targets.map((target) => (
                  <li
                    key={target.id}
                    className={cn('flex items-center gap-4 rounded-lg border bg-card p-4')}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[14.5px] font-medium text-foreground">
                        {target.name}
                      </div>
                      {target.description ? (
                        <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                          {target.description}
                        </p>
                      ) : null}
                      <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Users className="size-3.5" />
                        {tWorkspace(($) => $.join_page.members, {
                          count: target.member_count ?? 0,
                        })}
                      </p>
                    </div>
                    <Button onClick={() => onJoin(target.id)} disabled={pendingId !== null}>
                      {pendingId === target.id
                        ? tWorkspace(($) => $.join_page.joining)
                        : tWorkspace(($) => $.join_page.join)}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {failed ? (
            <p className="mt-4 text-sm text-destructive">{tWorkspace(($) => $.join_page.failed)}</p>
          ) : null}

          {onCreateInstead && !workspaceCreationDisabled ? (
            <div className="mt-6">
              <Button
                variant="link"
                className="px-0 text-muted-foreground"
                onClick={onCreateInstead}
              >
                {tWorkspace(($) => $.join_page.create_instead)}
              </Button>
            </div>
          ) : null}
        </div>
      </main>
    </div>
  );
}
