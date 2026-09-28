'use client';

import { useStore } from 'zustand';
import { ListTodo } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import {
  myIssuesRelationFromScope,
  myIssuesViewStore,
} from '@goosar/core/issues/stores/my-issues-view-store';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@goosar/ui/components/ui/empty';
import { PageHeader } from '../../layout/page-header';
import { IssueSurface } from '../../issues/surface/issue-surface';
import { useT } from '../../i18n';
import { MyIssuesHeader } from './my-issues-header';

export function MyIssuesPage() {
  const { t } = useT('my-issues');
  const user = useAuthStore((s) => s.user);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);
  const setScope = useStore(myIssuesViewStore, (s) => s.setScope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb)}</h1>
      </PageHeader>

      {user ? (
        <IssueSurface
          scope={{
            type: 'my',
            userId: user.id,
            relation: myIssuesRelationFromScope(scope),
          }}
          modes={['board', 'list', 'table', 'swimlane']}
          batchToolbar="list"
          renderHeader={({ controller }) => (
            <MyIssuesHeader
              allIssues={controller.surfaceIssues}
              scope={scope}
              onScopeChange={setScope}
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
                <EmptyDescription>{t(($) => $.page.empty_description)}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        />
      ) : null}
    </div>
  );
}
