'use client';

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Loader2, RefreshCw, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import {
  billingBalanceOptions,
  billingBatchesOptions,
  billingCheckoutSessionOptions,
  billingPriceTiersOptions,
  billingTopupsOptions,
  billingTransactionsOptions,
  useCreateCloudBillingCheckoutSession,
  useCreateCloudBillingPortalSession,
  useInvalidateBillingDataAfterCredit,
} from '@goosar/core/billing';
import type {
  BillingBatch,
  BillingPriceTier,
  BillingTopup,
  BillingTransaction,
} from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@goosar/ui/components/ui/card';
import { useT } from '../i18n';
import { useNavigation } from '../navigation';

const MICRO_PER_CREDIT = 1_000_000;
const CENTS_PER_DOLLAR = 100;

export function BillingTestPage() {
  const { t } = useT('billing');
  const { searchParams, replace, pathname } = useNavigation();

  const sessionId = searchParams.get('session_id') ?? '';

  return (
    <div className="space-y-6 p-6">
      <header>
        <h1 className="text-xl font-semibold">{t(($) => $.title)}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t(($) => $.subtitle)}</p>
      </header>

      {sessionId && (
        <CheckoutSessionStatusBanner
          sessionId={sessionId}
          onDismiss={() => {
            replace(pathname);
          }}
        />
      )}

      <BalanceCard />

      <BuyAndPortalSection />

      <TransactionsCard />

      <BatchesCard />

      <TopupsCard />
    </div>
  );
}

function CheckoutSessionStatusBanner({
  sessionId,
  onDismiss,
}: {
  sessionId: string;
  onDismiss: () => void;
}) {
  const { t } = useT('billing');
  const { data, isLoading, isError, error } = useQuery(billingCheckoutSessionOptions(sessionId));

  const status = data?.status ?? (isLoading ? 'loading' : '');
  const terminal = status === 'credited' || status === 'failed' || status === 'canceled';

  const invalidateBillingDataAfterCredit = useInvalidateBillingDataAfterCredit();
  useEffect(() => {
    if (terminal) invalidateBillingDataAfterCredit();
  }, [terminal, invalidateBillingDataAfterCredit]);

  return (
    <Card className="border-primary/40 bg-primary/5">
      <CardHeader>
        <CardTitle className="text-sm">
          {t(($) => $.checkout.session_label, { prefix: sessionId.slice(0, 16) })}
        </CardTitle>
        <CardDescription className="text-xs">
          {isLoading
            ? t(($) => $.checkout.loading)
            : isError
              ? t(($) => $.checkout.fetch_failed, {
                  error:
                    error instanceof Error
                      ? error.message
                      : t(($) => $.checkout.fetch_failed_unknown),
                })
              : terminal
                ? t(($) => $.checkout.final_status, { status })
                : t(($) => $.checkout.polling_status, {
                    status: status || t(($) => $.checkout.status_unknown),
                  })}
        </CardDescription>
      </CardHeader>
      {data && (
        <CardContent className="text-xs">
          <dl className="grid grid-cols-[120px_1fr] gap-y-1">
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_order)}</dt>
            <dd className="font-mono">{data.order_id}</dd>
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_tier)}</dt>
            <dd>{data.tier_id}</dd>
            <dt className="text-muted-foreground">{t(($) => $.checkout.label_charged)}</dt>
            <dd>
              {data.bonus_credits > 0
                ? t(($) => $.checkout.charged_with_bonus, {
                    money: formatMoney(data.amount_cents, data.currency),
                    credits: data.credits.toLocaleString(),
                    bonus: data.bonus_credits.toLocaleString(),
                  })
                : t(($) => $.checkout.charged_value, {
                    money: formatMoney(data.amount_cents, data.currency),
                    credits: data.credits.toLocaleString(),
                  })}
            </dd>
          </dl>
          {terminal && (
            <Button variant="outline" size="sm" className="mt-3" onClick={onDismiss}>
              {t(($) => $.checkout.clear_url)}
            </Button>
          )}
        </CardContent>
      )}
    </Card>
  );
}

function BalanceCard() {
  const { t } = useT('billing');
  const balance = useQuery(billingBalanceOptions());

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.balance.title)}</CardTitle>
          <CardDescription className="text-xs">{t(($) => $.endpoints.balance)}</CardDescription>
        </div>
        <RefreshButton isLoading={balance.isFetching} onClick={() => void balance.refetch()} />
      </CardHeader>
      <CardContent>
        {balance.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : balance.isError ? (
          <ErrorText error={balance.error} />
        ) : (
          <div className="space-y-1 text-sm">
            <div className="text-2xl font-semibold tabular-nums">
              {balance.data?.balance_credit.toLocaleString() ?? 0}
              <span className="ml-1 text-sm font-normal text-muted-foreground">
                {t(($) => $.balance.credits_suffix)}
              </span>
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.balance.meta, {
                micro: balance.data?.balance_micro.toLocaleString() ?? 0,
                owner: balance.data?.owner_id.slice(0, 8) ?? '',
                updated: formatDate(balance.data?.updated_at, t),
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function BuyAndPortalSection() {
  const { t } = useT('billing');
  const tiers = useQuery(billingPriceTiersOptions());
  const createCheckout = useCreateCloudBillingCheckoutSession();
  const createPortal = useCreateCloudBillingPortalSession();
  const [busyTier, setBusyTier] = useState<string | null>(null);

  const handleBuy = async (tier: BillingPriceTier) => {
    setBusyTier(tier.id);
    try {
      const { url } = await createCheckout.mutateAsync({ tier_id: tier.id });
      if (!url) {
        toast.error(t(($) => $.buy.toast_no_url));
        return;
      }
      window.location.href = url;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.buy.toast_checkout_failed));
    } finally {
      setBusyTier(null);
    }
  };

  const handlePortal = async () => {
    try {
      const { url } = await createPortal.mutateAsync();
      if (!url) {
        toast.error(t(($) => $.buy.toast_no_portal_url));
        return;
      }
      window.open(url, '_blank', 'noopener,noreferrer');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.buy.toast_portal_failed));
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{t(($) => $.buy.title)}</CardTitle>
        <CardDescription className="text-xs">{t(($) => $.endpoints.buy)}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {tiers.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : tiers.isError ? (
          <ErrorText error={tiers.error} />
        ) : tiers.data?.length ? (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {tiers.data.map((tier) => (
              <TierButton
                key={tier.id}
                tier={tier}
                busy={busyTier === tier.id}
                disabled={busyTier !== null}
                onClick={() => void handleBuy(tier)}
              />
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">{t(($) => $.buy.no_tiers)}</p>
        )}

        <div className="border-t pt-4">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={createPortal.isPending}
            onClick={() => void handlePortal()}
          >
            {createPortal.isPending ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
            )}
            {t(($) => $.buy.open_portal)}
          </Button>
          <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.buy.portal_hint)}</p>
        </div>
      </CardContent>
    </Card>
  );
}

function TierButton({
  tier,
  busy,
  disabled,
  onClick,
}: {
  tier: BillingPriceTier;
  busy: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  const { t } = useT('billing');
  const display = tier.display_name || tier.id;
  const baseLine = t(($) => $.buy.tier_money_to_credits, {
    money: formatMoney(tier.amount_cents, 'usd'),
    credits: tier.credits.toLocaleString(),
  });
  const bonusLine = tier.bonus_credits
    ? tier.bonus_expires_in
      ? t(($) => $.buy.tier_bonus_with_expiry, {
          credits: tier.bonus_credits.toLocaleString(),
          expiry: tier.bonus_expires_in,
        })
      : t(($) => $.buy.tier_bonus, {
          credits: tier.bonus_credits.toLocaleString(),
        })
    : '';
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="rounded-md border bg-background p-3 text-left transition hover:border-primary disabled:cursor-not-allowed disabled:opacity-50"
    >
      <div className="flex items-center justify-between">
        <div className="text-sm font-medium">{display}</div>
        {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {baseLine}
        {bonusLine}
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.buy.tier_id, { id: tier.id })}
      </div>
    </button>
  );
}

function TransactionsCard() {
  const { t } = useT('billing');
  const txs = useQuery(billingTransactionsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.transactions.title)}</CardTitle>
          <CardDescription className="text-xs">
            {t(($) => $.endpoints.transactions)}
          </CardDescription>
        </div>
        <RefreshButton isLoading={txs.isFetching} onClick={() => void txs.refetch()} />
      </CardHeader>
      <CardContent>
        {txs.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : txs.isError ? (
          <ErrorText error={txs.error} />
        ) : txs.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {txs.data.items.map((row) => (
              <TransactionRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>{t(($) => $.transactions.empty)}</EmptyText>
        )}
        <PagingFooter
          page={txs.data?.page ?? 1}
          pageSize={txs.data?.page_size ?? 20}
          total={txs.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function TransactionRow({ row }: { row: BillingTransaction }) {
  const { t } = useT('billing');
  const credit = row.amount_micro / MICRO_PER_CREDIT;
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.tx_type}
          <span className="ml-1.5 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
            {row.source}
          </span>
        </span>
        <span
          className={`text-sm tabular-nums ${
            credit >= 0 ? 'text-success' : 'text-destructive'
          }`}
        >
          {t(($) => $.transactions.credits_value, {
            value: `${credit >= 0 ? '+' : ''}${credit.toLocaleString()}`,
          })}
        </span>
      </div>
      {row.description && (
        <div className="mt-1 text-xs text-muted-foreground">{row.description}</div>
      )}
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.transactions.row_meta, {
          date: formatDate(row.created_at, t),
          balance: (row.balance_after / MICRO_PER_CREDIT).toLocaleString(),
          ref: row.reference_id || t(($) => $.transactions.ref_empty),
        })}
      </div>
    </li>
  );
}

function BatchesCard() {
  const { t } = useT('billing');
  const batches = useQuery(billingBatchesOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.batches.title)}</CardTitle>
          <CardDescription className="text-xs">{t(($) => $.endpoints.batches)}</CardDescription>
        </div>
        <RefreshButton isLoading={batches.isFetching} onClick={() => void batches.refetch()} />
      </CardHeader>
      <CardContent>
        {batches.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : batches.isError ? (
          <ErrorText error={batches.error} />
        ) : batches.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {batches.data.items.map((row) => (
              <BatchRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>{t(($) => $.batches.empty)}</EmptyText>
        )}
        <PagingFooter
          page={batches.data?.page ?? 1}
          pageSize={batches.data?.page_size ?? 20}
          total={batches.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function BatchRow({ row }: { row: BillingBatch }) {
  const { t } = useT('billing');
  const total = row.total_micro / MICRO_PER_CREDIT;
  const remaining = row.remaining_micro / MICRO_PER_CREDIT;
  const consumed = total - remaining;
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.source_type}
          <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">
            {t(($) => $.batches.id_suffix, { id: row.id.slice(0, 8) })}
          </span>
        </span>
        <span className="text-sm tabular-nums">
          {t(($) => $.batches.remaining_over_total, {
            remaining: remaining.toLocaleString(),
            total: total.toLocaleString(),
          })}
        </span>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {t(($) => $.batches.consumed, { value: consumed.toLocaleString() })}
        {row.expires_at
          ? t(($) => $.batches.expires_suffix, { value: formatDate(row.expires_at, t) })
          : t(($) => $.batches.never_expires_suffix)}
      </div>
    </li>
  );
}

function TopupsCard() {
  const { t } = useT('billing');
  const topups = useQuery(billingTopupsOptions({ page: 1, page_size: 20 }));
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-sm">{t(($) => $.topups.title)}</CardTitle>
          <CardDescription className="text-xs">{t(($) => $.endpoints.topups)}</CardDescription>
        </div>
        <RefreshButton isLoading={topups.isFetching} onClick={() => void topups.refetch()} />
      </CardHeader>
      <CardContent>
        {topups.isLoading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : topups.isError ? (
          <ErrorText error={topups.error} />
        ) : topups.data?.items.length ? (
          <ul className="space-y-2 text-xs">
            {topups.data.items.map((row) => (
              <TopupRow key={row.id} row={row} />
            ))}
          </ul>
        ) : (
          <EmptyText>{t(($) => $.topups.empty)}</EmptyText>
        )}
        <PagingFooter
          page={topups.data?.page ?? 1}
          pageSize={topups.data?.page_size ?? 20}
          total={topups.data?.total ?? 0}
        />
      </CardContent>
    </Card>
  );
}

function TopupRow({ row }: { row: BillingTopup }) {
  const { t } = useT('billing');
  return (
    <li className="rounded-md border bg-background p-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium">
          {row.tier_id || row.id.slice(0, 8)}
          <span
            className={`ml-1.5 rounded px-1.5 py-0.5 font-mono text-[10px] ${
              row.status === 'credited'
                ? 'bg-success/10 text-success'
                : row.status === 'failed' || row.status === 'canceled'
                  ? 'bg-destructive/10 text-destructive'
                  : 'bg-warning/10 text-warning'
            }`}
          >
            {row.status}
          </span>
        </span>
        <span className="text-sm tabular-nums">
          {row.bonus_credits > 0
            ? t(($) => $.topups.amount_to_credits_with_bonus, {
                money: formatMoney(row.amount_cents, row.currency),
                credits: row.credits.toLocaleString(),
                bonus: row.bonus_credits,
              })
            : t(($) => $.topups.amount_to_credits, {
                money: formatMoney(row.amount_cents, row.currency),
                credits: row.credits.toLocaleString(),
              })}
        </span>
      </div>
      <div className="mt-1 font-mono text-[10px] text-muted-foreground/70">
        {t(($) => $.topups.row_meta, {
          date: formatDate(row.created_at, t),
          checkout: row.stripe_checkout_id || t(($) => $.topups.stripe_empty),
        })}
      </div>
    </li>
  );
}

function PagingFooter({
  page,
  pageSize,
  total,
}: {
  page: number;
  pageSize: number;
  total: number;
}) {
  const { t } = useT('billing');
  if (total === 0) return null;
  return (
    <div className="mt-3 text-[10px] text-muted-foreground">
      {t(($) => $.shared.paging, {
        page,
        totalPages: Math.max(1, Math.ceil(total / pageSize)),
        total,
      })}
    </div>
  );
}

function RefreshButton({ isLoading, onClick }: { isLoading: boolean; onClick: () => void }) {
  const { t } = useT('billing');
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-7 w-7 p-0"
      onClick={onClick}
      disabled={isLoading}
      aria-label={t(($) => $.shared.refresh)}
    >
      <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? 'animate-spin' : ''}`} />
    </Button>
  );
}

function ErrorText({ error }: { error: unknown }) {
  const { t } = useT('billing');
  return (
    <p className="text-xs text-destructive">
      {error instanceof Error ? error.message : t(($) => $.shared.request_failed)}
    </p>
  );
}

function EmptyText({ children }: { children: React.ReactNode }) {
  return <p className="text-xs text-muted-foreground">{children}</p>;
}

function formatMoney(amountCents: number, currency: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currency.toUpperCase(),
    }).format(amountCents / CENTS_PER_DOLLAR);
  } catch {
    return `${(amountCents / CENTS_PER_DOLLAR).toFixed(2)} ${currency.toUpperCase()}`;
  }
}

function formatDate(value: string | undefined, t: ReturnType<typeof useT<'billing'>>['t']): string {
  if (!value) return t(($) => $.shared.date_dash);
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString();
}
