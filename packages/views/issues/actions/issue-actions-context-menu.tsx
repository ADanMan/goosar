'use client';

import {
  cloneElement,
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react';
import type { Issue } from '@goosar/core/types';
import { ContextMenu, ContextMenuContent } from '@goosar/ui/components/ui/context-menu';
import { useIssueActions } from './use-issue-actions';
import { IssueActionsMenuItems, contextPrimitives } from './issue-actions-menu-items';
import { AssigneePicker } from '../components/pickers';

interface ActiveMenu {
  issue: Issue;
  position: { x: number; y: number };
}

type OpenIssueContextMenu = (issue: Issue, event: React.MouseEvent) => void;

const IssueContextMenuContext = createContext<OpenIssueContextMenu | null>(null);

export function IssueContextMenuProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<ActiveMenu | null>(null);
  const [open, setOpen] = useState(false);
  const triggerElRef = useRef<HTMLElement | null>(null);

  const openMenu = useCallback<OpenIssueContextMenu>((issue, event) => {
    event.preventDefault();
    triggerElRef.current?.removeAttribute('data-popup-open');
    const el = event.currentTarget as HTMLElement;
    el.setAttribute('data-popup-open', '');
    triggerElRef.current = el;
    setActive({ issue, position: { x: event.clientX, y: event.clientY } });
    setOpen(true);
  }, []);

  const handleOpenChange = useCallback((v: boolean) => {
    if (!v) {
      triggerElRef.current?.removeAttribute('data-popup-open');
      triggerElRef.current = null;
    }
    setOpen(v);
  }, []);

  return (
    <IssueContextMenuContext.Provider value={openMenu}>
      {children}
      {/* Mounted on first use, kept mounted after (one closed menu root per
          surface — the popup itself unmounts while closed). */}
      {active && (
        <IssueContextMenuSingleton
          issue={active.issue}
          position={active.position}
          open={open}
          onOpenChange={handleOpenChange}
        />
      )}
    </IssueContextMenuContext.Provider>
  );
}

function IssueContextMenuSingleton({
  issue,
  position,
  open,
  onOpenChange,
}: ActiveMenu & {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const actions = useIssueActions(issue);
  const [assigneeOpen, setAssigneeOpen] = useState(false);

  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () =>
        DOMRect.fromRect({
          x: position.x,
          y: position.y,
          width: 0,
          height: 0,
        }),
    }),
    [position.x, position.y],
  );

  return (
    <>
      <ContextMenu open={open} onOpenChange={(v) => onOpenChange(v)}>
        <ContextMenuContent anchor={anchor}>
          <IssueActionsMenuItems
            issue={issue}
            actions={actions}
            primitives={contextPrimitives}
            onOpenAssignee={() => setAssigneeOpen(true)}
          />
        </ContextMenuContent>
      </ContextMenu>
      {/* Mount the picker only once the user actually opens it, anchored at
          the right-click position so it opens where the context menu just
          was instead of jumping to the row's top-left corner. */}
      {assigneeOpen && (
        <AssigneePicker
          assigneeType={issue.assignee_type}
          assigneeId={issue.assignee_id}
          onUpdate={actions.updateField}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={
            <span
              aria-hidden
              className="pointer-events-none fixed"
              style={{
                left: position.x,
                top: position.y,
                width: 0,
                height: 0,
              }}
            />
          }
          trigger={<span />}
          align="start"
        />
      )}
    </>
  );
}

interface IssueActionsContextMenuProps {
  issue: Issue;
  children: ReactElement<{
    onContextMenu?: (e: React.MouseEvent) => void;
    className?: string;
  }>;
}

export function IssueActionsContextMenu({ issue, children }: IssueActionsContextMenuProps) {
  const openMenu = useContext(IssueContextMenuContext);
  if (!openMenu) {
    throw new Error('IssueActionsContextMenu requires an IssueContextMenuProvider ancestor');
  }
  const childOnContextMenu = children.props.onContextMenu;
  const childClassName = children.props.className;
  return cloneElement(children, {
    className: childClassName ? `${childClassName} select-none` : 'select-none',
    onContextMenu: (e: React.MouseEvent) => {
      childOnContextMenu?.(e);
      openMenu(issue, e);
    },
  });
}
