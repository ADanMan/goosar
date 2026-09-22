import { useAuthStore } from '@goosar/core/auth';
import { browserTimezone } from './timezone-select';

export function useViewingTimezone(): string {
  const stored = useAuthStore((s) => s.user?.timezone ?? null);
  if (stored && stored.trim() !== '') return stored;
  return browserTimezone();
}
