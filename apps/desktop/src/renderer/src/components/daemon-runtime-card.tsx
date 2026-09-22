import { useState, useEffect, useCallback, useMemo } from 'react';
import {
  AlertCircle,
  Play,
  Square,
  RotateCw,
  Activity,
  ScrollText,
  LogIn,
  Info,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useWorkspaceId } from '@goosar/core/hooks';
import { runtimeListOptions } from '@goosar/core/runtimes';
import { agentTaskSnapshotOptions } from '@goosar/core/agents';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { toast } from 'sonner';
import { DaemonPanel } from './daemon-panel';
import { reauthenticateDaemon } from '../platform/daemon-reauth';
import type { DaemonStatus } from '../../../shared/daemon-types';
import { daemonStateLabel } from './agent-runtime-copy';
import { useT } from '@goosar/views/i18n';

export function DaemonRuntimeActions() {
  const { t } = useT('settings');
  const [status, setStatus] = useState<DaemonStatus>({ state: 'stopped' });
  const [panelOpen, setPanelOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);

  const wsId = useWorkspaceId();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const localRuntimeIds = useMemo(() => {
    if (!status.daemonId) return new Set<string>();
    return new Set(runtimes.filter((r) => r.daemon_id === status.daemonId).map((r) => r.id));
  }, [runtimes, status.daemonId]);

  const runtimeCount = localRuntimeIds.size;

  const affectedTasks = useMemo(
    () =>
      snapshot.filter(
        (t) =>
          localRuntimeIds.has(t.runtime_id) &&
          (t.status === 'running' || t.status === 'dispatched'),
      ),
    [snapshot, localRuntimeIds],
  );

  useEffect(() => {
    window.daemonAPI.getStatus().then((s) => setStatus(s));
    const unsub = window.daemonAPI.onStatusChange((s) => {
      setStatus(s);
      setActionLoading(false);
    });
    return unsub;
  }, []);

  const handleStart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.start();
    if (!result.success) {
      setActionLoading(false);
      toast.error(
        t(($) => $.desktop.daemon.actions.start_failed),
        {
          description: result.error,
        },
      );
    }
  }, [t]);

  const performStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    if (!result.success) {
      toast.error(
        t(($) => $.desktop.daemon.actions.stop_failed),
        {
          description: result.error,
        },
      );
    }
  }, [t]);

  const handleStopClick = useCallback(() => {
    if (affectedTasks.length === 0) {
      void performStop();
    } else {
      setConfirmStop(true);
    }
  }, [affectedTasks.length, performStop]);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.restart();
    if (!result.success) {
      toast.error(
        t(($) => $.desktop.daemon.actions.restart_failed),
        {
          description: result.error,
        },
      );
      return;
    }
    toast.success(
      t(($) => $.desktop.daemon.actions.restarting),
      {
        description: t(($) => $.desktop.daemon.actions.restarting_note),
      },
    );
  }, [t]);

  const handleRetryInstall = useCallback(async () => {
    setActionLoading(true);
    try {
      await window.daemonAPI.retryInstall();
    } finally {
      setActionLoading(false);
    }
  }, []);

  const handleReauth = useCallback(async () => {
    setActionLoading(true);
    await reauthenticateDaemon();
    setActionLoading(false);
  }, []);

  const isRunning = status.state === 'running';
  const externallyManaged = status.externallyManaged === true;
  const isStopped = status.state === 'stopped';
  const isCliMissing = status.state === 'cli_not_found';
  const isAuthExpired = status.state === 'auth_expired';
  const isTransitioning = status.state === 'starting' || status.state === 'stopping';
  const isInstalling = status.state === 'installing_cli';

  return (
    <>
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {isRunning && (
          <>
            <Button size="sm" variant="ghost" onClick={() => setPanelOpen(true)}>
              <ScrollText className="size-3.5 mr-1.5" />
              {t(($) => $.desktop.daemon.actions.view_logs)}
            </Button>
            {externallyManaged ? (
              <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                <Info className="size-3.5 shrink-0" />
                {t(($) => $.desktop.daemon.actions.externally_managed)}
              </span>
            ) : (
              <>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleRestart}
                  disabled={actionLoading}
                >
                  <RotateCw className="size-3.5 mr-1.5" />
                  {t(($) => $.desktop.daemon.actions.restart)}
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={handleStopClick}
                  disabled={actionLoading}
                >
                  <Square className="size-3.5 mr-1.5" />
                  {t(($) => $.desktop.daemon.actions.stop)}
                </Button>
              </>
            )}
          </>
        )}

        {isStopped && (
          <Button size="sm" onClick={handleStart} disabled={actionLoading}>
            {actionLoading ? (
              <Activity className="size-3.5 mr-1.5 animate-pulse" />
            ) : (
              <Play className="size-3.5 mr-1.5" />
            )}
            {t(($) => $.desktop.daemon.actions.start)}
          </Button>
        )}

        {isCliMissing && (
          <Button size="sm" variant="outline" onClick={handleRetryInstall} disabled={actionLoading}>
            <RotateCw className="size-3.5 mr-1.5" />
            {t(($) => $.desktop.daemon.actions.retry_setup)}
          </Button>
        )}

        {isAuthExpired && (
          <>
            <span className="inline-flex items-center gap-1.5 text-xs text-destructive">
              <AlertCircle className="size-3.5 shrink-0" />
              {t(($) => $.desktop.daemon.actions.auth_expired)}
            </span>
            <Button size="sm" onClick={handleReauth} disabled={actionLoading}>
              {actionLoading ? (
                <Activity className="size-3.5 mr-1.5 animate-pulse" />
              ) : (
                <LogIn className="size-3.5 mr-1.5" />
              )}
              {t(($) => $.desktop.daemon.actions.sign_in_again)}
            </Button>
          </>
        )}

        {(isTransitioning || isInstalling) && (
          <Button size="sm" variant="outline" disabled>
            <Activity className="size-3.5 mr-1.5 animate-pulse" />
            {daemonStateLabel(t, status.state)}
          </Button>
        )}
      </div>

      <DaemonPanel
        open={panelOpen}
        onOpenChange={setPanelOpen}
        status={status}
        runtimeCount={runtimeCount}
      />

      <StopConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        affectedCount={affectedTasks.length}
        onConfirm={() => {
          setConfirmStop(false);
          void performStop();
        }}
      />
    </>
  );
}

function StopConfirmDialog({
  open,
  onOpenChange,
  affectedCount,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  affectedCount: number;
  onConfirm: () => void;
}) {
  const { t } = useT('settings');

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-sm font-semibold">
              {t(($) => $.desktop.daemon.actions.confirm_stop_title, {
                count: affectedCount,
              })}
            </DialogTitle>
            <DialogDescription className="text-xs leading-relaxed">
              {t(($) => $.desktop.daemon.actions.confirm_stop_body, {
                count: affectedCount,
              })}
            </DialogDescription>
          </DialogHeader>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.desktop.daemon.actions.confirm_stop_cancel)}
          </Button>
          <Button variant="destructive" onClick={onConfirm}>
            {t(($) => $.desktop.daemon.actions.confirm_stop_confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
