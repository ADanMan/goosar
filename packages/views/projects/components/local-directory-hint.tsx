'use client';

import { useQuery } from '@tanstack/react-query';
import { FolderOpen } from 'lucide-react';
import { projectResourcesOptions } from '@goosar/core/projects';
import type { LocalDirectoryResourceRef, ProjectResource } from '@goosar/core/types';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useLocalDaemonStatus } from '../../platform';
import { useT } from '../../i18n';

export function LocalDirectoryHint({ projectId }: { projectId: string | null | undefined }) {
  const { t } = useT('projects');
  const wsId = useWorkspaceId();
  const daemon = useLocalDaemonStatus();
  const { data: resources = [] } = useQuery({
    ...projectResourcesOptions(wsId, projectId ?? ''),
    enabled: Boolean(projectId),
  });

  if (!projectId) return null;
  if (!daemon.daemonId) return null;

  const matches: Array<ProjectResource & { resource_ref: LocalDirectoryResourceRef }> = resources
    .filter(
      (r): r is ProjectResource & { resource_ref: LocalDirectoryResourceRef } =>
        r.resource_type === 'local_directory',
    )
    .filter((r) => r.resource_ref.daemon_id === daemon.daemonId);

  if (matches.length === 0) return null;

  return (
    <div className="mt-3 space-y-1 rounded-md border border-dashed bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
      {matches.map((resource) => {
        const ref = resource.resource_ref;
        const label = (ref.label || resource.label || ref.local_path).trim() || ref.local_path;
        return (
          <div key={resource.id} className="flex items-center gap-2">
            <FolderOpen className="size-3 shrink-0" />
            <span className="truncate">
              {t(($) => $.resources.chat_hint_prefix)}
              <span className="font-medium text-foreground"> {label} </span>
              <span className="font-mono opacity-70">({ref.local_path})</span>
            </span>
          </div>
        );
      })}
    </div>
  );
}
