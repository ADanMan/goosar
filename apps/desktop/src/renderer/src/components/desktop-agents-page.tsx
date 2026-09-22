import { useEffect, useState } from 'react';
import { AgentsPage } from '@goosar/views/agents';
import type { DaemonStatus } from '../../../shared/daemon-types';
import { useRuntimeConnectExtras } from '../platform/use-launcher-extras';

export function DesktopAgentsPage() {
  const [status, setStatus] = useState<DaemonStatus>({ state: 'stopped' });
  const [lastIdentity, setLastIdentity] = useState<{
    daemonId: string | null;
    deviceName: string | null;
  }>({ daemonId: null, deviceName: null });
  const [hostName, setHostName] = useState<string | null>(null);
  const runtimeConnectExtras = useRuntimeConnectExtras();

  useEffect(() => {
    const apply = (s: DaemonStatus) => {
      setStatus(s);
      if (s.daemonId) {
        setLastIdentity({
          daemonId: s.daemonId,
          deviceName: s.deviceName ?? null,
        });
      }
    };
    window.daemonAPI.getStatus().then(apply);
    window.daemonAPI.getHostName().then((name) => setHostName(name || null));
    return window.daemonAPI.onStatusChange(apply);
  }, []);

  return (
    <AgentsPage
      localDaemonId={status.daemonId ?? lastIdentity.daemonId}
      localMachineName={status.deviceName ?? lastIdentity.deviceName ?? hostName}
      hasLocalMachine
      runtimeConnectExtras={runtimeConnectExtras}
    />
  );
}
