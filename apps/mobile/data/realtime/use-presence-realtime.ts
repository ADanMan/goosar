// Realtime присутствия агентов: инвалидирует запросы, стоящие за индикатором
// присутствия.
import { useQueryClient } from "@tanstack/react-query";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function usePresenceRealtime() {
  const queryClient = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const runtimesKey = ["runtimes", wsId];
      const agentsKey = ["agents", wsId];
      const snapshotKey = ["agent-task-snapshot", wsId];

      const invalidateRuntimes = () =>
        queryClient.invalidateQueries({ queryKey: runtimesKey });
      const invalidateAgents = () =>
        queryClient.invalidateQueries({ queryKey: agentsKey });
      const invalidateSnapshot = () =>
        queryClient.invalidateQueries({ queryKey: snapshotKey });

      return [
        ws.on("daemon:register", invalidateRuntimes),

        ws.on("agent:status", invalidateAgents),
        ws.on("agent:created", invalidateAgents),
        ws.on("agent:archived", invalidateAgents),
        ws.on("agent:restored", invalidateAgents),

        ws.on("task:queued", invalidateSnapshot),
        ws.on("task:dispatch", invalidateSnapshot),
        ws.on("task:completed", invalidateSnapshot),
        ws.on("task:failed", invalidateSnapshot),
        ws.on("task:cancelled", invalidateSnapshot),

        ws.onReconnect(() => {
          invalidateRuntimes();
          invalidateSnapshot();
        }),
      ];
    },
    [queryClient],
  );
}
