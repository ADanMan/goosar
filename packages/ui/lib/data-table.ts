import type { Column, RowData } from '@tanstack/react-table';
import type * as React from 'react';

declare module '@tanstack/react-table' {
  interface ColumnMeta<TData extends RowData, TValue> {
    grow?: boolean;
  }
}

export function getCellStyle<TData>(
  column: Column<TData>,
  options?: { withBorder?: boolean; hasExplicitSize?: boolean },
): React.CSSProperties {
  const grow = column.columnDef.meta?.grow;
  const width = grow && !options?.hasExplicitSize ? undefined : column.getSize();

  const isPinned = column.getIsPinned();
  if (!isPinned) {
    return width !== undefined ? { width } : {};
  }

  const withBorder = options?.withBorder ?? false;
  const isLastLeftPinned = isPinned === 'left' && column.getIsLastColumn('left');
  const isFirstRightPinned = isPinned === 'right' && column.getIsFirstColumn('right');

  return {
    width,
    position: 'sticky',
    left: isPinned === 'left' ? `${column.getStart('left')}px` : undefined,
    right: isPinned === 'right' ? `${column.getAfter('right')}px` : undefined,
    zIndex: 1,
    boxShadow: withBorder
      ? isLastLeftPinned
        ? '-4px 0 4px -4px var(--border) inset'
        : isFirstRightPinned
          ? '4px 0 4px -4px var(--border) inset'
          : undefined
      : undefined,
  };
}
