'use client';

import { memo, useCallback, useMemo, useState, type ReactNode } from 'react';
import { Virtuoso } from 'react-virtuoso';
import { EyeOff, MoreHorizontal, Plus, UserMinus } from 'lucide-react';
import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import type { Issue, IssueAssigneeType, IssueStatus, Project } from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@goosar/ui/components/ui/dropdown-menu';
import { STATUS_CONFIG } from '@goosar/core/issues/config';
import { useViewStoreApi } from '@goosar/core/issues/stores/view-store-context';
import { StatusHeading } from './status-heading';
import { DraggableBoardCard } from './board-card';
import type { ChildProgress } from './list-row';
import { useT } from '../../i18n';
import { ActorAvatar } from '../../common/actor-avatar';
import { useRestoredScrollOffset, useRestoredScrollRef } from '../../platform';
import { DeferredPopup } from '../../common/deferred-popup';
import { DeferredTooltip } from '../../common/deferred-tooltip';
import { VirtuosoSeed } from '../../common/virtuoso-seed';
import type { IssueCreateDefaults } from '../surface/types';

export const BOARD_COL_WIDTH = 280;
export const BOARD_CARD_WIDTH = BOARD_COL_WIDTH - 16 - 8; 

const BOARD_SEED_COUNT = 10;

const BOARD_CARD_ESTIMATED_HEIGHT = 110;

const BOARD_VIRTUALIZE_THRESHOLD = 30;

const EMPTY_VIRTUOSO_COMPONENTS = {};

export interface BoardColumnGroup {
  id: string;
  title: string;
  status?: IssueStatus;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  propertyId?: string;
  propertyOptionId?: string | null;
  propertyOptionColor?: string;
  totalCount?: number;
  createData?: IssueCreateDefaults;
}

export const BoardColumn = memo(function BoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  totalCount,
  footer,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  totalCount?: number;
  footer?: ReactNode;
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const status = group.status;
  const cfg = status ? STATUS_CONFIG[status] : null;
  const { setNodeRef, isOver } = useDroppable({ id: group.id });
  const viewStoreApi = useViewStoreApi();
  const { t } = useT('issues');

  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((id) => {
        const issue = issueMap.get(id);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const scrollMementoKey = `board:${group.id}`;
  const restoredScrollTop = useRestoredScrollOffset(scrollMementoKey);
  const restoreScrollRef = useRestoredScrollRef(scrollMementoKey);
  const mergedRef = useCallback(
    (el: HTMLDivElement | null) => {
      setNodeRef(el);
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [setNodeRef, restoreScrollRef],
  );
  const footerComponents = useMemo(
    () => (footer ? { Footer: () => <>{footer}</> } : EMPTY_VIRTUOSO_COMPONENTS),
    [footer],
  );

  const computeItemKey = (_index: number, issue: Issue) => issue.id;
  const itemContent = (index: number, issue: Issue) => (
    <div className={index === 0 ? undefined : 'pt-2'}>
      <DraggableBoardCard
        issue={issue}
        childProgress={childProgressMap?.get(issue.id)}
        project={issue.project_id ? projectMap?.get(issue.project_id) : undefined}
        disableSorting={!!sortLabel}
      />
    </div>
  );

  return (
    <div
      style={{ width: BOARD_COL_WIDTH }}
      className={`flex shrink-0 flex-col rounded-xl ${cfg?.columnBg ?? 'bg-muted/40'} p-2`}
    >
      <div className="mb-2 flex items-center justify-between px-1.5">
        <BoardGroupHeading group={group} count={totalCount ?? issueIds.length} />

        {/* Right: add + menu */}
        <div className="flex items-center gap-1">
          {/* Column-header popups mount lazily: a board/swimlane renders one
              header per column and almost none of these menus/tooltips are
              ever opened — eagerly mounting them dominated surface mount
              cost (DeferredPopup / DeferredTooltip). */}
          {status && (
            <DeferredPopup
              ariaHasPopup="menu"
              triggerRender={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                >
                  <MoreHorizontal className="size-3.5" />
                </Button>
              }
            >
              {(open, onOpenChange) => (
                <DropdownMenu open={open} onOpenChange={onOpenChange}>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="rounded-full text-muted-foreground"
                      >
                        <MoreHorizontal className="size-3.5" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => viewStoreApi.getState().hideStatus(status)}>
                      <EyeOff className="size-3.5" />
                      {t(($) => $.board.hide_column)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DeferredPopup>
          )}
          {onCreateIssue && (
            <DeferredTooltip
              content={t(($) => $.board.add_issue_tooltip)}
              trigger={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  onClick={() => {
                    const data = {
                      ...(group.createData ?? {}),
                      ...(projectId ? { project_id: projectId } : {}),
                    };
                    onCreateIssue(data);
                  }}
                >
                  <Plus className="size-3.5" />
                </Button>
              }
            />
          )}
        </div>
      </div>
      <div className="relative min-h-[200px] flex-1 rounded-lg">
        {isOver && sortLabel && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg bg-background/40">
            <span className="rounded-md bg-popover px-2.5 py-1 text-xs font-medium text-popover-foreground shadow-sm border border-border">
              {sortLabel}
            </span>
          </div>
        )}
        <div
          ref={mergedRef}
          data-tab-scroll-root={scrollMementoKey}
          className={`absolute inset-0 overflow-y-auto rounded-lg p-1 transition-colors ${
            isOver && sortLabel ? 'ring-2 ring-brand/25 bg-accent/15' : isOver ? 'bg-accent/60' : ''
          }`}
        >
          {resolvedIssues.length > 0 ? (
            <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
              {resolvedIssues.length <= BOARD_VIRTUALIZE_THRESHOLD ? (
                <>
                  <VirtuosoSeed
                    data={resolvedIssues}
                    itemContent={itemContent}
                    computeItemKey={computeItemKey}
                    count={resolvedIssues.length}
                  />
                  {footer}
                </>
              ) : scrollEl ? (
                <Virtuoso
                  customScrollParent={scrollEl}
                  data={resolvedIssues}
                  computeItemKey={computeItemKey}
                  initialScrollTop={restoredScrollTop}
                  initialItemCount={Math.min(resolvedIssues.length, BOARD_SEED_COUNT)}
                  defaultItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                  increaseViewportBy={{ top: 300, bottom: 300 }}
                  components={footerComponents}
                  itemContent={itemContent}
                />
              ) : (
                <VirtuosoSeed
                  data={resolvedIssues}
                  itemContent={itemContent}
                  computeItemKey={computeItemKey}
                  count={BOARD_SEED_COUNT}
                  estimatedItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                />
              )}
            </SortableContext>
          ) : (
            <>
              {issueIds.length === 0 && (
                <p className="py-8 text-center text-xs text-muted-foreground">
                  {t(($) => $.board.empty_column)}
                </p>
              )}
              {footer}
            </>
          )}
        </div>
      </div>
    </div>
  );
});

function BoardGroupHeading({ group, count }: { group: BoardColumnGroup; count: number }) {
  if (group.status) {
    return <StatusHeading status={group.status} count={count} />;
  }

  if (group.propertyId !== undefined) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        <span
          className="size-2.5 shrink-0 rounded-full bg-muted-foreground/30"
          style={
            group.propertyOptionColor ? { backgroundColor: group.propertyOptionColor } : undefined
          }
        />
        <span className="truncate text-sm font-medium" title={group.title}>
          {group.title}
        </span>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
          {count}
        </span>
      </div>
    );
  }

  const actorIcon =
    group.assigneeType && group.assigneeId ? (
      <ActorAvatar
        actorType={group.assigneeType}
        actorId={group.assigneeId}
        size="sm"
        showStatusDot={group.assigneeType === 'agent'}
      />
    ) : (
      <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
        <UserMinus className="size-3.5" />
      </span>
    );

  return (
    <div className="flex min-w-0 items-center gap-2">
      {actorIcon}
      <span className="truncate text-sm font-medium" title={group.title}>
        {group.title}
      </span>
      <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}
