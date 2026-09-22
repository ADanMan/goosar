import { useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ApiError } from '@goosar/core/api';
import { cronPreviewOptions } from '@goosar/core/autopilots/queries';
import { useT } from '../../../i18n';
import type { ScheduleConfig } from './model';
import { toCron } from './cron-mapping';

export interface ScheduleRejection {
  code: 'invalid_cron' | 'invalid_timezone';
  detail: string;
}

export function classifyScheduleRejection(err: ApiError): ScheduleRejection {
  const body = typeof err.body === 'object' && err.body !== null ? err.body : {};
  const code = (body as { code?: unknown }).code;
  return {
    code: code === 'invalid_timezone' ? 'invalid_timezone' : 'invalid_cron',
    detail: err.message,
  };
}

export async function findScheduleRejection(
  queryClient: QueryClient,
  wsId: string,
  config: ScheduleConfig,
): Promise<ScheduleRejection | null> {
  try {
    await queryClient.fetchQuery(cronPreviewOptions(wsId, toCron(config), config.timezone));
    return null;
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 400) return null;
    return classifyScheduleRejection(err);
  }
}

export function toastScheduleRejection(
  t: ReturnType<typeof useT<'autopilots'>>['t'],
  rejection: ScheduleRejection,
): void {
  toast.error(
    rejection.code === 'invalid_timezone'
      ? t(($) => $.schedule_editor.timezone_invalid)
      : t(($) => $.schedule_editor.cron_invalid),
    { description: rejection.detail },
  );
}

export function useScheduleSubmitGate(wsId: string): {
  scheduleValid: boolean;
  clearRejection: () => void;
  onValidityChange: (valid: boolean) => void;
  ensureAccepted: (config: ScheduleConfig) => Promise<boolean>;
} {
  const { t } = useT('autopilots');
  const queryClient = useQueryClient();
  const [scheduleValid, setScheduleValid] = useState(true);
  return {
    scheduleValid,
    clearRejection: () => setScheduleValid(true),
    onValidityChange: setScheduleValid,
    ensureAccepted: async (config: ScheduleConfig): Promise<boolean> => {
      const rejection = await findScheduleRejection(queryClient, wsId, config);
      if (rejection === null) return true;
      setScheduleValid(false);
      toastScheduleRejection(t, rejection);
      return false;
    },
  };
}
