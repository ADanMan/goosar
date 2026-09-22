// Realtime-провайдер (второй слой): владеет WSClient, решает, когда быть
// подключённым, и отдаёт клиент через контекст хукам третьего слоя.
import {
  createContext,
  use,
  useEffect,
  useRef,
  useState,
} from "react";
import { AppState, type AppStateStatus } from "react-native";
import NetInfo from "@react-native-community/netinfo";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { getToken } from "@/data/secure-storage";
import { WSClient } from "./ws-client";

const API_URL = process.env.EXPO_PUBLIC_API_URL;

if (!API_URL) {
  throw new Error("EXPO_PUBLIC_API_URL is not set");
}

const WS_URL = `${API_URL.replace(/^http/, "ws")}/ws`;

const RealtimeContext = createContext<WSClient | null>(null);

export function useWSClient(): WSClient | null {
  return use(RealtimeContext);
}

export function RealtimeProvider({ children }: { children: React.ReactNode }) {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [client, setClient] = useState<WSClient | null>(null);

  const lastConnectedRef = useRef<boolean | null>(null);

  useEffect(() => {
    if (!userId || !wsSlug) {
      setClient(null);
      return;
    }

    let cancelled = false;
    let ws: WSClient | null = null;
    let appStateSub: { remove: () => void } | null = null;
    let netInfoUnsub: (() => void) | null = null;

    void (async () => {
      const token = await getToken();
      if (cancelled || !token) return;

      ws = new WSClient({
        url: WS_URL,
        token,
        workspaceSlug: wsSlug,
        clientVersion: "0.1.0",
        logger: console,
      });
      ws.connect();
      setClient(ws);

      appStateSub = AppState.addEventListener(
        "change",
        (status: AppStateStatus) => {
          if (status === "active") {
            ws?.resume();
            ws?.forceReconnect();
          } else if (status === "background") {
            ws?.pause();
          }
          // 'inactive' (iOS-only, transient) → ignore.
        },
      );

      lastConnectedRef.current = null;
      netInfoUnsub = NetInfo.addEventListener((state) => {
        const isConnected = state.isConnected === true;
        const previous = lastConnectedRef.current;
        lastConnectedRef.current = isConnected;
        if (previous === false && isConnected) {
          console.info("[realtime] netinfo: back online → forceReconnect");
          ws?.forceReconnect();
        }
      });
    })();

    return () => {
      cancelled = true;
      appStateSub?.remove();
      netInfoUnsub?.();
      ws?.disconnect();
      setClient(null);
    };
  }, [userId, wsSlug]);

  return (
    <RealtimeContext.Provider value={client}>
      {children}
    </RealtimeContext.Provider>
  );
}
