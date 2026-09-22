'use client';

import { Fragment, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { PageHeader } from './page-header';
import { AppLink } from '../navigation';

export interface BreadcrumbSegment {
  href: string;
  label: ReactNode;
  className?: string;
}

interface BreadcrumbHeaderProps {
  segments: BreadcrumbSegment[];
  leaf: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function BreadcrumbHeader({ segments, leaf, actions, className }: BreadcrumbHeaderProps) {
  return (
    <PageHeader className={cn('gap-2 bg-background text-sm', className)}>
      <div className="flex flex-1 items-center gap-1.5 min-w-0">
        {segments.map((segment) => (
          <Fragment key={segment.href}>
            <AppLink
              href={segment.href}
              className={cn(
                'text-muted-foreground hover:text-foreground transition-colors',
                segment.className ?? 'shrink-0',
              )}
            >
              {segment.label}
            </AppLink>
            <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
          </Fragment>
        ))}
        {leaf}
      </div>
      {actions ? <div className="flex items-center gap-1 shrink-0">{actions}</div> : null}
    </PageHeader>
  );
}
