'use client';

import { useT } from '../../i18n';
import { InfiniteScrollSentinel } from './infinite-scroll-sentinel';

const PAGINATED_THRESHOLD = 50;

export function ListLoadMoreFooter({
  hasMore,
  isLoading,
  total,
  onLoadMore,
  isError = false,
  onRetry,
}: {
  hasMore: boolean;
  isLoading: boolean;
  total: number;
  onLoadMore: () => void;
  isError?: boolean;
  onRetry?: () => void;
}) {
  const { t } = useT('issues');

  if (isError && onRetry) {
    return (
      <button
        type="button"
        className="w-full py-2 text-xs text-destructive hover:underline"
        onClick={onRetry}
      >
        {t(($) => $.table.load_more_failed_retry)}
      </button>
    );
  }

  if (hasMore) {
    return (
      <InfiniteScrollSentinel
        onVisible={onLoadMore}
        loading={isLoading}
        label={t(($) => $.table.loading_branch)}
      />
    );
  }

  if (total > PAGINATED_THRESHOLD) {
    return (
      <div className="py-2 text-center text-xs text-muted-foreground/70">
        {t(($) => $.table.no_more)}
      </div>
    );
  }

  return null;
}
