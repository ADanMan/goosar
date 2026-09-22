import { toast } from 'sonner';
import { describeApiFailure } from '@goosar/core/api';

export function toastApiError(err: unknown, fallback: string): void {
  const failure = describeApiFailure(err);
  const text = failure.message ?? fallback;
  toast.error(text, failure.detail === text ? undefined : { description: failure.detail });
}
