import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';

export const billingKeys = {
  all: () => ['billing'] as const,
  balance: () => [...billingKeys.all(), 'balance'] as const,
  transactions: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), 'transactions', params ?? {}] as const,
  batches: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), 'batches', params ?? {}] as const,
  topups: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), 'topups', params ?? {}] as const,
  priceTiers: () => [...billingKeys.all(), 'price-tiers'] as const,
  checkoutSession: (sessionId: string) =>
    [...billingKeys.all(), 'checkout-session', sessionId] as const,
};

export function billingBalanceOptions() {
  return queryOptions({
    queryKey: billingKeys.balance(),
    queryFn: () => api.getCloudBillingBalance(),
    staleTime: 30 * 1000,
  });
}

export function billingTransactionsOptions(params?: { page?: number; page_size?: number }) {
  return queryOptions({
    queryKey: billingKeys.transactions(params),
    queryFn: () => api.listCloudBillingTransactions(params),
    staleTime: 30 * 1000,
  });
}

export function billingBatchesOptions(params?: { page?: number; page_size?: number }) {
  return queryOptions({
    queryKey: billingKeys.batches(params),
    queryFn: () => api.listCloudBillingBatches(params),
    staleTime: 30 * 1000,
  });
}

export function billingTopupsOptions(params?: { page?: number; page_size?: number }) {
  return queryOptions({
    queryKey: billingKeys.topups(params),
    queryFn: () => api.listCloudBillingTopups(params),
    staleTime: 30 * 1000,
  });
}

export function billingPriceTiersOptions() {
  return queryOptions({
    queryKey: billingKeys.priceTiers(),
    queryFn: () => api.listCloudBillingPriceTiers(),
    staleTime: 5 * 60 * 1000,
  });
}

export function billingCheckoutSessionOptions(sessionId: string) {
  return queryOptions({
    queryKey: billingKeys.checkoutSession(sessionId),
    queryFn: () => api.getCloudBillingCheckoutSession(sessionId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status === 'credited' || status === 'failed' || status === 'canceled') {
        return false;
      }
      return 2000;
    },
    staleTime: 0,
  });
}
