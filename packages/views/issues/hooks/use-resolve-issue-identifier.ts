'use client';

import { useQuery } from '@tanstack/react-query';
import { issueIdentifierOptions } from '@goosar/core/issues/queries';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { isIssueIdentifier } from '@goosar/ui/markdown';
import type { Issue } from '@goosar/core/types';

export function useResolveIssueIdentifier(identifier: string): Issue | null {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? '';
  const prefix = workspace?.issue_prefix;
  const prefixMatches = !prefix || identifier.toUpperCase().startsWith(`${prefix.toUpperCase()}-`);

  const { data } = useQuery({
    ...issueIdentifierOptions(wsId, identifier),
    enabled: Boolean(wsId) && isIssueIdentifier(identifier) && prefixMatches,
  });

  return data ?? null;
}
