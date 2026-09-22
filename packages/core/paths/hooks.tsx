'use client';

import { createContext, use, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Workspace } from '../types';
import { workspaceListOptions } from '../workspace/queries';
import { paths, type WorkspacePaths } from './paths';

const WorkspaceSlugContext = createContext<string | null>(null);

export function WorkspaceSlugProvider({
  slug,
  children,
}: {
  slug: string | null;
  children: ReactNode;
}) {
  return <WorkspaceSlugContext.Provider value={slug}>{children}</WorkspaceSlugContext.Provider>;
}

export function useWorkspaceSlug(): string | null {
  return use(WorkspaceSlugContext);
}

export function useRequiredWorkspaceSlug(): string {
  const slug = useWorkspaceSlug();
  if (!slug) {
    throw new Error('useRequiredWorkspaceSlug called outside a workspace-scoped route');
  }
  return slug;
}

export function useCurrentWorkspace(): Workspace | null {
  const slug = useWorkspaceSlug();
  const { data: list = [] } = useQuery(workspaceListOptions());
  if (!slug) return null;
  return list.find((w) => w.slug === slug) ?? null;
}

export function useWorkspacePaths(): WorkspacePaths {
  const slug = useRequiredWorkspaceSlug();
  return paths.workspace(slug);
}
