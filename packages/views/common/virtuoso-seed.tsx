'use client';

import { Fragment, type Key, type ReactNode } from 'react';

export const VIRTUOSO_SEED_COUNT = 30;

export function VirtuosoSeed<T>({
  data,
  itemContent,
  computeItemKey,
  count = VIRTUOSO_SEED_COUNT,
  estimatedItemHeight,
}: {
  data: T[];
  itemContent: (index: number, item: T) => ReactNode;
  computeItemKey: (index: number, item: T) => Key;
  count?: number;
  estimatedItemHeight?: number;
}) {
  const seeded = data.slice(0, count);
  const remaining = data.length - seeded.length;
  return (
    <>
      {seeded.map((item, index) => (
        <Fragment key={computeItemKey(index, item)}>{itemContent(index, item)}</Fragment>
      ))}
      {estimatedItemHeight !== undefined && remaining > 0 && (
        <div aria-hidden style={{ height: remaining * estimatedItemHeight }} />
      )}
    </>
  );
}
