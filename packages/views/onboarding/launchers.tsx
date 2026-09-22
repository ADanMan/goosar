'use client';

import { useCallback, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Plug, Wrench } from 'lucide-react';
import { runtimeKeys } from '@goosar/core/runtimes/queries';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import type { AgentRuntime } from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import { Dialog, DialogContent, DialogTitle } from '@goosar/ui/components/ui/dialog';
import { StepRuntimeConnect } from './steps/step-runtime-connect';
import { StepWorkTools } from './steps/step-work-tools';
import { useT } from '../i18n';

export interface WorkToolsLauncherExtras {
  perimeterCaMissing?: boolean;
  installedMcpNames?: string[];
}

export interface RuntimeConnectLauncherExtras {
  onRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
  onSaveLlmConnection?: React.ComponentProps<typeof StepRuntimeConnect>['onSaveLlmConnection'];
  localDaemonId?: string | null;
  localMachineName?: string | null;
  perimeterMachine?: React.ComponentProps<typeof StepRuntimeConnect>['perimeterMachine'];
  existingLlmConnection?: React.ComponentProps<typeof StepRuntimeConnect>['existingLlmConnection'];
  daemonState?: string | null;
  networkStatusSlot?: ReactNode;
}

function StepOverlay({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent
        className="inset-0 top-0 left-0 h-dvh w-dvw max-w-none translate-x-0 translate-y-0 gap-0 overflow-y-auto rounded-none p-0 ring-0"
        showCloseButton
      >
        {/* The steps render their own headings; this one exists so the dialog
            has an accessible name without a second visible title. */}
        <DialogTitle className="sr-only">{title}</DialogTitle>
        {children}
      </DialogContent>
    </Dialog>
  );
}

export function WorkToolsSetupButton({
  wsId,
  extras,
  variant = 'outline',
  size = 'sm',
}: {
  wsId: string;
  extras?: WorkToolsLauncherExtras;
  variant?: React.ComponentProps<typeof Button>['variant'];
  size?: React.ComponentProps<typeof Button>['size'];
}) {
  const { t } = useT('onboarding');
  const [open, setOpen] = useState(false);
  const label = t(($) => $.launchers.configure_work_tools);

  return (
    <>
      <Button variant={variant} size={size} onClick={() => setOpen(true)}>
        <Wrench className="size-3.5" />
        {label}
      </Button>
      {open ? (
        <StepOverlay open onClose={() => setOpen(false)} title={label}>
          <StepWorkTools
            wsId={wsId}
            runtime={null}
            onFinish={() => setOpen(false)}
            perimeterCaMissing={extras?.perimeterCaMissing}
            installedMcpNames={
              extras?.installedMcpNames?.length ? extras.installedMcpNames : undefined
            }
          />
        </StepOverlay>
      ) : null}
    </>
  );
}

export function RuntimeConnectButton({
  wsId,
  extras,
  variant = 'outline',
  size = 'sm',
}: {
  wsId: string;
  extras?: RuntimeConnectLauncherExtras;
  variant?: React.ComponentProps<typeof Button>['variant'];
  size?: React.ComponentProps<typeof Button>['size'];
}) {
  const { t } = useT('onboarding');
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const label = t(($) => $.launchers.connect_machine);

  const close = useCallback(
    (_runtime?: AgentRuntime | null) => {
      setOpen(false);
      void queryClient.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      void queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
    [queryClient, wsId],
  );

  return (
    <>
      <Button variant={variant} size={size} onClick={() => setOpen(true)}>
        <Plug className="size-3.5" />
        {label}
      </Button>
      {open ? (
        <StepOverlay open onClose={close} title={label}>
          <StepRuntimeConnect
            wsId={wsId}
            onNext={close}
            onRefresh={extras?.onRefresh}
            runtimesPending={extras?.runtimesPending}
            onSaveLlmConnection={extras?.onSaveLlmConnection}
            localDaemonId={extras?.localDaemonId}
            localMachineName={extras?.localMachineName}
            perimeterMachine={extras?.perimeterMachine}
            existingLlmConnection={extras?.existingLlmConnection}
            daemonState={extras?.daemonState}
            networkStatusSlot={extras?.networkStatusSlot}
          />
        </StepOverlay>
      ) : null}
    </>
  );
}
