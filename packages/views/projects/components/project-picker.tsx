'use client';

import { useState } from 'react';
import { FolderKanban, X } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { projectListOptions } from '@goosar/core/projects/queries';
import { useWorkspaceId } from '@goosar/core/hooks';
import type { UpdateIssueRequest } from '@goosar/core/types';
import { cn } from '@goosar/ui/lib/utils';
import { ProjectIcon } from './project-icon';
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from '../../issues/components/pickers/property-picker';
import { matchesPinyin } from '../../editor/extensions/pinyin-match';
import { useT } from '../../i18n';

export function ProjectPicker({
  projectId,
  onUpdate,
  triggerRender,
  align = 'start',
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange,
  disabled = false,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: 'start' | 'center' | 'end';
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  disabled?: boolean;
}) {
  const { t } = useT('projects');
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = projects.find((p) => p.id === projectId);
  const [filter, setFilter] = useState('');
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = disabled ? false : (controlledOpen ?? internalOpen);
  const setOpen = disabled ? () => {} : (onOpenChange ?? setInternalOpen);

  const query = filter.trim().toLowerCase();
  const filtered = projects.filter(
    (p) => p.title.toLowerCase().includes(query) || matchesPinyin(p.title, query),
  );

  const resolvedTriggerRender = triggerRender ?? (
    <button
      type="button"
      disabled={disabled}
      className={cn(PICKER_TRIGGER_CLASS, current && 'pr-5')}
    />
  );

  return (
    <div className="group/project relative inline-flex min-w-0">
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        width="w-52"
        align={align}
        searchable
        searchPlaceholder={t(($) => $.picker.search_placeholder)}
        onSearchChange={setFilter}
        triggerRender={resolvedTriggerRender}
        trigger={
          current ? (
            <>
              <ProjectIcon project={current} size="sm" />
              <span className="truncate">{current.title}</span>
            </>
          ) : (
            <>
              <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{t(($) => $.picker.no_project)}</span>
            </>
          )
        }
      >
        {/* "No project" clear row — hidden while searching, mirrors the
            unassigned row in the assignee picker. */}
        {!query && projects.length > 0 && (
          <PickerItem
            selected={!projectId}
            onClick={() => {
              onUpdate({ project_id: null });
              setOpen(false);
            }}
          >
            <FolderKanban className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">{t(($) => $.picker.no_project)}</span>
          </PickerItem>
        )}

        {filtered.map((p) => (
          <PickerItem
            key={p.id}
            selected={p.id === projectId}
            onClick={() => {
              onUpdate({ project_id: p.id });
              setOpen(false);
            }}
          >
            <ProjectIcon project={p} size="sm" />
            <span className="truncate">{p.title}</span>
          </PickerItem>
        ))}

        {projects.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
        {projects.length > 0 && filtered.length === 0 && query && <PickerEmpty />}
      </PropertyPicker>

      {current && (
        <button
          type="button"
          disabled={disabled}
          aria-label={t(($) => $.picker.remove)}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onUpdate({ project_id: null });
          }}
          className="pointer-events-none absolute right-1 top-1/2 flex size-3.5 -translate-y-1/2 items-center justify-center rounded-sm text-muted-foreground opacity-0 transition-[background-color,color,opacity] hover:bg-muted-foreground/20 hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none group-hover/project:pointer-events-auto group-hover/project:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100 disabled:pointer-events-none disabled:opacity-0 disabled:group-hover/project:opacity-0"
        >
          <X className="size-2.5" />
        </button>
      )}
    </div>
  );
}
