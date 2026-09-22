import { RuntimesPage } from '@goosar/views/runtimes';
import { useDesktopRuntimeContext } from './use-desktop-runtime-context';

export function DesktopRuntimesPage() {
  const context = useDesktopRuntimeContext();

  return (
    <RuntimesPage
      localDaemonId={context.localDaemonId}
      localMachineName={context.localMachineName}
      hasLocalMachine
      bootstrapping={context.bootstrapping}
    />
  );
}
