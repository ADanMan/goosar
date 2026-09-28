'use client';

import { ListTodo } from 'lucide-react';
import type { Issue, IssueTableFacetSpec, IssueTableFacetsResponse } from '@goosar/core/types';
import { useIssuesScopeStore } from '@goosar/core/issues/stores/issues-scope-store';
import { useViewStore } from '@goosar/core/issues/stores/view-store-context';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@goosar/ui/components/ui/empty';
import { openCreateIssueWithPreference } from '@goosar/core/issues/stores/create-mode-store';
import { PageHeader } from '../../layout/page-header';
import { useT } from '../../i18n';
import { IssueSurface } from '../surface/issue-surface';
import { IssuesHeader } from './issues-header';

function IssuesSurfaceHeader({
  issues,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);

  return (
    <IssuesHeader
      scopedIssues={issues}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      facetCountsExact={facetCountsExact}
      tableFacetCounts={tableFacetCounts}
      onTableFacetChange={onTableFacetChange}
    />
  );
}

export function IssuesPage() {
  const { t } = useT('issues');
  const scope = useIssuesScopeStore((s) => s.scope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <IssueSurface
        scope={{ type: 'workspace', actorKind: scope }}
        modes={['board', 'list', 'table', 'swimlane']}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={() => (
          <Empty className="flex-1 border-0">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ListTodo />
              </EmptyMedia>
              <EmptyTitle>{t(($) => $.page.empty_title)}</EmptyTitle>
              <EmptyDescription>{t(($) => $.page.empty_hint)}</EmptyDescription>
            </EmptyHeader>
            {/* Atlassian empty-states pattern — an empty state needs one
                primary action, not just a description. */}
            <EmptyContent>
              <Button size="sm" onClick={() => openCreateIssueWithPreference()}>
                {t(($) => $.page.empty_action)}
              </Button>
            </EmptyContent>
          </Empty>
        )}
      />
    </div>
  );
}
