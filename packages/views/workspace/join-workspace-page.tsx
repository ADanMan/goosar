'use client';

import { useState } from 'react';
import { LogOut, Users } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@goosar/ui/components/ui/button';
import { paths } from '@goosar/core/paths';
import { useConfigStore } from '@goosar/core/config';
import { joinTargetListOptions } from '@goosar/core/workspace/queries';
import { useJoinWorkspace } from '@goosar/core/workspace/mutations';
import { useNavigation } from '../navigation';
import { useLogout } from '../auth';
import { DragStrip } from '../platform';
import { useT } from '../i18n';

export function JoinWorkspacePage({ onCreateInstead }: { onCreateInstead?: () => void }) {
  const { t } = useT('workspace');
  const nav = useNavigation();
  const logout = useLogout();
  const { data: targets = [], isLoading } = useQuery(joinTargetListOptions());
  const join = useJoinWorkspace();
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const showCreateInstead = !!onCreateInstead && !workspaceCreationDisabled;

  const onJoin = (id: string) => {
    setPendingId(id);
    setFailed(false);
    join.mutate(id, {
      onSuccess: (result) => {
        setPendingId(null);
        nav.replace(result.slug ? paths.workspace(result.slug).issues() : paths.root());
      },
      onError: () => {
        setPendingId(null);
        setFailed(true);
      },
    });
  };

  return (
    <div className="relative flex min-h-svh flex-col bg-background">
      <DragStrip />
      <Button
        variant="ghost"
        size="sm"
        className="absolute top-16 right-12 text-muted-foreground hover:text-destructive"
        onClick={logout}
      >
        <LogOut />
        {t(($) => $.new_page.log_out)}
      </Button>

      <div className="flex flex-1 flex-col items-center justify-center px-6 py-16">
        <div className="flex w-full max-w-2xl flex-col gap-6">
          <div className="text-center">
            <h1 className="text-3xl font-semibold tracking-tight">{t(($) => $.join_page.title)}</h1>
            <p className="mt-3 text-muted-foreground">{t(($) => $.join_page.description)}</p>
          </div>

          {isLoading ? (
            <p className="text-center text-sm text-muted-foreground">
              {t(($) => $.join_page.loading)}
            </p>
          ) : targets.length === 0 ? (
            <p className="text-center text-sm text-muted-foreground">
              {t(($) => $.join_page.empty)}
            </p>
          ) : (
            <ul className="flex flex-col gap-3">
              {targets.map((target) => (
                <li
                  key={target.id}
                  className="flex items-center gap-4 rounded-lg border border-border bg-card p-4"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{target.name}</div>
                    {target.description ? (
                      <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                        {target.description}
                      </p>
                    ) : null}
                    <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
                      <Users className="size-3.5" />
                      {t(($) => $.join_page.members, {
                        count: target.member_count ?? 0,
                      })}
                    </p>
                  </div>
                  <Button onClick={() => onJoin(target.id)} disabled={pendingId !== null}>
                    {pendingId === target.id
                      ? t(($) => $.join_page.joining)
                      : t(($) => $.join_page.join)}
                  </Button>
                </li>
              ))}
            </ul>
          )}

          {failed ? (
            <p className="text-center text-sm text-destructive">{t(($) => $.join_page.failed)}</p>
          ) : null}

          {showCreateInstead ? (
            <div className="text-center">
              <Button variant="link" className="text-muted-foreground" onClick={onCreateInstead}>
                {t(($) => $.join_page.create_instead)}
              </Button>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
