'use client';

import {
  flexRender,
  type Header as TanstackHeader,
  type Row,
  type Table as TanstackTable,
} from '@tanstack/react-table';
import { useVirtualizer } from '@tanstack/react-virtual';
import * as React from 'react';

import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@goosar/ui/components/ui/table';
import { getCellStyle } from '@goosar/ui/lib/data-table';
import { cn } from '@goosar/ui/lib/utils';

interface DataTableProps<TData> extends React.ComponentProps<'div'> {
  table: TanstackTable<TData>;
  actionBar?: React.ReactNode;
  emptyMessage?: React.ReactNode;
  onRowClick?: (row: Row<TData>) => void;
  renderRow?: (row: Row<TData>) => React.ReactNode;
  footer?: React.ReactNode;
  virtualizeRows?: boolean;
  virtualRowHeight?: number;
  virtualOverscan?: number;
}

export function DataTable<TData>({
  table,
  actionBar,
  emptyMessage = 'No results.',
  onRowClick,
  renderRow,
  footer,
  virtualizeRows = false,
  virtualRowHeight = 41,
  virtualOverscan = 10,
  className,
  ...props
}: DataTableProps<TData>) {
  const [resizingColumnId, setResizingColumnId] = React.useState<string | null>(null);

  const columnSizing = table.getState().columnSizing;
  const hasExplicitSize = React.useCallback(
    (columnId: string) => Object.prototype.hasOwnProperty.call(columnSizing, columnId),
    [columnSizing],
  );

  const setColumnWidth = React.useCallback(
    (header: TanstackHeader<TData, unknown>, width: number) => {
      const minSize = header.column.columnDef.minSize ?? 48;
      const maxSize = header.column.columnDef.maxSize ?? Number.MAX_SAFE_INTEGER;
      const next = Math.min(maxSize, Math.max(minSize, Math.round(width)));

      table.setColumnSizing((old) => ({
        ...old,
        [header.column.id]: next,
      }));
    },
    [table],
  );

  const beginColumnResize = React.useCallback(
    (header: TanstackHeader<TData, unknown>, event: React.PointerEvent<HTMLDivElement>) => {
      if (!header.column.getCanResize()) return;

      event.preventDefault();
      event.stopPropagation();

      const startX = event.clientX;
      const headerCell = event.currentTarget.closest('th');
      const startWidth = headerCell?.getBoundingClientRect().width ?? header.column.getSize();

      setResizingColumnId(header.column.id);
      setColumnWidth(header, startWidth);

      const originalCursor = document.body.style.cursor;
      const originalUserSelect = document.body.style.userSelect;
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';

      const handlePointerMove = (pointerEvent: PointerEvent) => {
        setColumnWidth(header, startWidth + pointerEvent.clientX - startX);
      };

      const stopResize = () => {
        window.removeEventListener('pointermove', handlePointerMove);
        window.removeEventListener('pointerup', stopResize);
        window.removeEventListener('pointercancel', stopResize);
        document.body.style.cursor = originalCursor;
        document.body.style.userSelect = originalUserSelect;
        setResizingColumnId(null);
      };

      window.addEventListener('pointermove', handlePointerMove);
      window.addEventListener('pointerup', stopResize);
      window.addEventListener('pointercancel', stopResize);
    },
    [setColumnWidth],
  );

  const handleResizeKeyDown = React.useCallback(
    (header: TanstackHeader<TData, unknown>, event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;

      event.preventDefault();
      event.stopPropagation();

      const headerCell = event.currentTarget.closest('th');
      const currentWidth = hasExplicitSize(header.column.id)
        ? header.column.getSize()
        : (headerCell?.getBoundingClientRect().width ?? header.column.getSize());
      const direction = event.key === 'ArrowRight' ? 1 : -1;
      const step = event.shiftKey ? 20 : 8;

      setColumnWidth(header, currentWidth + direction * step);
    },
    [hasExplicitSize, setColumnWidth],
  );

  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const rows = table.getRowModel().rows;
  const getVirtualRowKey = React.useCallback((index: number) => rows[index]?.id ?? index, [rows]);
  const rowVirtualizer = useVirtualizer({
    count: virtualizeRows ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => virtualRowHeight,
    getItemKey: getVirtualRowKey,
    overscan: virtualOverscan,
  });
  const virtualItems = rowVirtualizer.getVirtualItems();
  const firstVirtualItem = virtualItems[0];
  const lastVirtualItem = virtualItems[virtualItems.length - 1];
  const virtualPaddingTop = firstVirtualItem?.start ?? 0;
  const virtualPaddingBottom = lastVirtualItem
    ? rowVirtualizer.getTotalSize() - lastVirtualItem.end
    : 0;

  const renderDataRow = (row: Row<TData>) => {
    const customRow = renderRow?.(row);
    if (customRow != null) {
      return <React.Fragment key={row.id}>{customRow}</React.Fragment>;
    }
    return (
      <TableRow
        key={row.id}
        data-state={row.getIsSelected() && 'selected'}
        onClick={onRowClick ? () => onRowClick(row) : undefined}
        className={cn('group', onRowClick && 'cursor-pointer')}
      >
        {row.getVisibleCells().map((cell) => {
          const isPinned = cell.column.getIsPinned();
          const columnHasExplicitSize = hasExplicitSize(cell.column.id);
          return (
            <TableCell
              key={cell.id}
              className={cn(
                'overflow-hidden px-4 py-2',
                isPinned &&
                  'bg-background group-hover:bg-[color-mix(in_oklab,var(--muted)_50%,var(--background))]',
              )}
              style={getCellStyle(cell.column, {
                withBorder: true,
                hasExplicitSize: columnHasExplicitSize,
              })}
            >
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </TableCell>
          );
        })}
      </TableRow>
    );
  };

  const renderVirtualSpacer = (position: 'top' | 'bottom', height: number) =>
    height > 0 ? (
      <TableRow
        key={`virtual-spacer-${position}`}
        aria-hidden
        className="pointer-events-none border-0 hover:bg-transparent"
      >
        <TableCell
          colSpan={table.getVisibleLeafColumns().length}
          className="p-0"
          style={{ height: `${height}px` }}
        />
      </TableRow>
    ) : null;

  return (
    <div className={cn('flex min-h-0 flex-1 flex-col', className)} {...props}>
      <div ref={scrollRef} className="flex min-h-0 flex-1 flex-col overflow-auto bg-background">
        <table
          className="w-full table-fixed caption-bottom text-sm"
          style={{ minWidth: `${table.getTotalSize()}px` }}
        >
          <TableHeader className="sticky top-0 z-10 bg-muted/30 backdrop-blur">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className="hover:bg-transparent">
                {headerGroup.headers.map((header) => {
                  const isPinned = header.column.getIsPinned();
                  const columnHasExplicitSize = hasExplicitSize(header.column.id);
                  const headerLabel =
                    typeof header.column.columnDef.header === 'string'
                      ? header.column.columnDef.header
                      : header.column.id;
                  return (
                    <TableHead
                      key={header.id}
                      colSpan={header.colSpan}
                      className={cn(
                        'relative h-8 overflow-hidden px-4 py-2 text-xs uppercase tracking-wider text-muted-foreground',
                        isPinned && 'bg-[color-mix(in_oklab,var(--muted)_30%,var(--background))]',
                      )}
                      style={getCellStyle(header.column, {
                        withBorder: true,
                        hasExplicitSize: columnHasExplicitSize,
                      })}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(header.column.columnDef.header, header.getContext())}
                      {!header.isPlaceholder && header.column.getCanResize() && (
                        <div
                          role="separator"
                          aria-label={`Resize ${headerLabel} column`}
                          aria-orientation="vertical"
                          tabIndex={0}
                          className={cn(
                            'absolute top-0 right-0 h-full w-2 cursor-col-resize touch-none select-none outline-none',
                            'after:absolute after:top-1/2 after:right-0 after:h-4 after:w-px after:-translate-y-1/2 after:bg-border after:opacity-0 after:transition-opacity',
                            'hover:after:opacity-100 focus-visible:after:opacity-100',
                            resizingColumnId === header.column.id &&
                              'after:bg-primary after:opacity-100',
                          )}
                          onPointerDown={(event) => beginColumnResize(header, event)}
                          onDoubleClick={(event) => {
                            event.preventDefault();
                            event.stopPropagation();
                            header.column.resetSize();
                          }}
                          onKeyDown={(event) => handleResizeKeyDown(header, event)}
                        />
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rows.length ? (
              virtualizeRows ? (
                <>
                  {renderVirtualSpacer('top', virtualPaddingTop)}
                  {virtualItems.map((virtualItem) => {
                    const row = rows[virtualItem.index];
                    return row ? renderDataRow(row) : null;
                  })}
                  {renderVirtualSpacer('bottom', virtualPaddingBottom)}
                </>
              ) : (
                rows.map(renderDataRow)
              )
            ) : (
              <TableRow>
                <TableCell
                  colSpan={table.getAllColumns().length}
                  className="h-24 text-center text-muted-foreground"
                >
                  {emptyMessage}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
          {footer}
        </table>
      </div>
      {actionBar && table.getFilteredSelectedRowModel().rows.length > 0 && actionBar}
    </div>
  );
}
