'use client';

import { ArrowDown, ArrowUp } from 'lucide-react';

import { cn } from '../../lib/utils';

export type ListGridSortDirection = 'asc' | 'desc';

function ListGrid({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="table"
      className={cn('grid w-full min-w-0 content-start gap-x-3', className)}
      {...props}
    />
  );
}

function ListGridHeader({ className, children, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="row"
      className={cn(
        'group/header sticky top-0 z-10 col-span-full grid h-9 grid-cols-subgrid items-center bg-background after:pointer-events-none after:absolute after:inset-x-0 after:top-full after:h-3 after:bg-gradient-to-b after:from-background after:to-transparent',
        className,
      )}
      {...props}
    >
      <span aria-hidden="true" />
      {children}
      <span aria-hidden="true" />
    </div>
  );
}

interface ListGridHeaderCellProps extends React.HTMLAttributes<HTMLDivElement> {
  sorted?: ListGridSortDirection | false;
  onSort?: () => void;
  align?: 'left' | 'right';
}

function ListGridHeaderCell({
  sorted = false,
  onSort,
  align = 'left',
  className,
  children,
  ...props
}: ListGridHeaderCellProps) {
  if (!onSort) {
    return (
      <div
        role="columnheader"
        className={cn(
          'flex min-w-0 items-center px-2 text-xs text-muted-foreground',
          align === 'right' && 'justify-end',
          className,
        )}
        {...props}
      >
        {children}
      </div>
    );
  }
  const Arrow = sorted === 'asc' ? ArrowUp : ArrowDown;
  return (
    <div
      role="columnheader"
      aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined}
      className={cn(
        'flex min-w-0 items-center px-2',
        align === 'right' && 'justify-end',
        className,
      )}
      {...props}
    >
      <button
        type="button"
        onClick={onSort}
        className={cn(
          'group/sort flex h-6 items-center gap-0.5 rounded-md text-xs transition-colors',
          sorted
            ? 'font-medium text-foreground'
            : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
          align === 'right' ? '-mr-1.5 flex-row-reverse pl-1 pr-1.5' : '-ml-1.5 pl-1.5 pr-1',
        )}
      >
        {children}
        <Arrow
          className={cn(
            'size-3 shrink-0',
            sorted ? 'opacity-100' : 'opacity-0 group-hover/sort:opacity-50',
          )}
        />
      </button>
    </div>
  );
}

function ListGridBody({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      role="rowgroup"
      className={cn('col-span-full grid grid-cols-subgrid content-start', className)}
      {...props}
    />
  );
}

export const LIST_GRID_BOTTOM_CLEARANCE = 64;

type ListGridRowProps = React.HTMLAttributes<HTMLDivElement>;

function ListGridRow({ className, children, ...props }: ListGridRowProps) {
  return (
    <div
      role="row"
      className={cn(
        'group/row col-span-full grid h-12 grid-cols-subgrid items-center transition-colors hover:bg-accent/40',
        className,
      )}
      {...props}
    >
      <span aria-hidden="true" />
      {children}
      <span aria-hidden="true" />
    </div>
  );
}

function ListGridCell({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div role="cell" className={cn('flex min-w-0 items-center px-2', className)} {...props} />;
}

export { ListGrid, ListGridBody, ListGridHeader, ListGridHeaderCell, ListGridRow, ListGridCell };
