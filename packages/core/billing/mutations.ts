import { useCallback } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import type { CreateBillingCheckoutSessionRequest } from '../types';
import { billingKeys } from './queries';

export function useCreateCloudBillingCheckoutSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateBillingCheckoutSessionRequest) =>
      api.createCloudBillingCheckoutSession(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: [...billingKeys.all(), 'topups'] });
    },
  });
}

export function useCreateCloudBillingPortalSession() {
  return useMutation({
    mutationFn: () => api.createCloudBillingPortalSession(),
    // No cache invalidation — the portal opens, the user does whatever,
    // and any state changes Stripe-side propagate back via webhook.
    // The next React Query refetch picks them up at its own cadence.
  });
}

export function useInvalidateBillingDataAfterCredit() {
  const qc = useQueryClient();
  return useCallback(() => {
    qc.invalidateQueries({ queryKey: billingKeys.balance() });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), 'transactions'] });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), 'batches'] });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), 'topups'] });
  }, [qc]);
}
