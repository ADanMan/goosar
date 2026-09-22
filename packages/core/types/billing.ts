// Отражает формы ответов модуля Billing в goosar-cloud

export interface BillingBalance {
  owner_id: string;
  balance_micro: number;
  balance_credit: number;
  updated_at: string;
}

export type BillingTxType = 'topup' | 'deduction' | 'refund' | 'expire' | 'adjustment';

export type BillingTxSource = 'gateway' | 'fleet' | 'topup' | 'refund' | 'admin' | 'system';

export interface BillingTransaction {
  id: string;
  owner_id: string;
  idempotency_key: string;
  tx_type: string;
  source: string;
  amount_micro: number;
  balance_after: number;
  reference_id: string;
  description: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface BillingTransactionsPage {
  items: BillingTransaction[];
  total: number;
  page: number;
  page_size: number;
}

export type BillingBatchSourceType = 'purchase' | 'bonus' | 'adjustment';

export interface BillingBatch {
  id: string;
  owner_id: string;
  source_tx_id: string;
  source_type: string;
  total_micro: number;
  remaining_micro: number;
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface BillingBatchesPage {
  items: BillingBatch[];
  total: number;
  page: number;
  page_size: number;
}

export type BillingTopupStatus = 'pending' | 'paid' | 'credited' | 'failed' | 'canceled';

export interface BillingTopup {
  id: string;
  owner_id: string;
  amount_cents: number;
  currency: string;
  credits: number;
  bonus_credits: number;
  status: string;
  tier_id: string;
  stripe_checkout_id: string;
  purchase_batch_id?: string;
  created_at: string;
  updated_at: string;
}

export interface BillingTopupsPage {
  items: BillingTopup[];
  total: number;
  page: number;
  page_size: number;
}

export interface BillingPriceTier {
  id: string;
  display_name: string;
  amount_cents: number;
  credits: number;
  bonus_credits?: number;
  bonus_expires_in?: string;
}

export interface CreateBillingCheckoutSessionRequest {
  tier_id: string;
  customer_email?: string;
}

export interface CreateBillingCheckoutSessionResponse {
  order_id: string;
  session_id: string;
  url: string;
}

export interface BillingCheckoutSessionStatus {
  order_id: string;
  status: string;
  amount_cents: number;
  credits: number;
  bonus_credits: number;
  currency: string;
  tier_id: string;
}

export interface CreateBillingPortalSessionResponse {
  url: string;
}
