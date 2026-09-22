import { useAuthStore } from '@goosar/core/auth';
import { toast } from 'sonner';

export async function reauthenticateDaemon(): Promise<void> {
  const user = useAuthStore.getState().user;
  const token = localStorage.getItem('goosar_token');
  if (!user || !token) {
    useAuthStore.getState().logout();
    return;
  }

  try {
    const result = await window.daemonAPI.reauthenticate(token, user.id);
    if (result.ok) return; 
    if (result.reason === 'session_invalid') {
      useAuthStore.getState().logout();
      return;
    }
    toast.error("Couldn't reconnect the daemon", {
      description: result.message || 'Please try again in a moment.',
    });
  } catch (err) {
    toast.error("Couldn't reconnect the daemon", {
      description: err instanceof Error ? err.message : 'Please try again.',
    });
  }
}
