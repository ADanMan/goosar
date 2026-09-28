-- 012_billing: usage-credit wallet. schemas.BillingBalance/BillingTransaction/
-- BillingBatch/BillingTopup. wallet_owner_key is a soft reference (not an FK): depending on
-- deployment mode the billing owner is either a workspace or a whole self-hosted
-- deployment, so it is kept as an opaque text key exactly as the contract types it,
-- resolved in application code. schemas.BillingPriceTier is static pricing config,
-- not workspace/user data, and is not persisted here.

CREATE TABLE wallet_balances (
    wallet_owner_key           text PRIMARY KEY,
    wb_balance_micro   numeric(20,6) NOT NULL DEFAULT 0,
    wb_balance_credit  numeric(20,6) NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE wallet_transactions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_owner_key            text NOT NULL,
    wt_idempotency_key  text NOT NULL,
    wt_tx_type          text NOT NULL CHECK (wt_tx_type IN ('topup','deduction','refund','expire','adjustment')),
    wt_source           text NOT NULL CHECK (wt_source IN ('gateway','fleet','topup','refund','admin','system')),
    wt_amount_micro     numeric(20,6) NOT NULL,
    wt_balance_after    numeric(20,6) NOT NULL,
    wt_reference_id     text,
    wt_description      text,
    wt_metadata         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallet_transactions_idem_uk UNIQUE (wallet_owner_key, wt_idempotency_key)
);
CREATE INDEX wallet_transactions_owner_ix ON wallet_transactions (wallet_owner_key, created_at DESC);

CREATE TABLE wallet_credit_batches (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_owner_key              text NOT NULL,
    wcb_source_tx_id      uuid REFERENCES wallet_transactions(id),
    wcb_source_type       text NOT NULL CHECK (wcb_source_type IN ('purchase','bonus','adjustment')),
    wcb_total_micro       numeric(20,6) NOT NULL,
    wcb_remaining_micro   numeric(20,6) NOT NULL,
    wcb_valid_until       timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_credit_batches_owner_ix ON wallet_credit_batches (wallet_owner_key) WHERE wcb_remaining_micro > 0;

CREATE TABLE wallet_topups (
    id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_owner_key                  text NOT NULL,
    wtu_amount_cents          integer NOT NULL,
    wtu_currency              text NOT NULL DEFAULT 'usd',
    wtu_credits               numeric(20,6) NOT NULL DEFAULT 0,
    wtu_bonus_credits         numeric(20,6) NOT NULL DEFAULT 0,
    wtu_status                text NOT NULL DEFAULT 'pending'
                              CHECK (wtu_status IN ('pending','paid','credited','failed','canceled')),
    wtu_tier_id               text,
    wtu_gateway_checkout_id   text,
    wtu_credit_batch_id       uuid REFERENCES wallet_credit_batches(id),
    created_at                timestamptz NOT NULL DEFAULT now(),
    updated_at                timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_topups_owner_ix ON wallet_topups (wallet_owner_key, created_at DESC);
