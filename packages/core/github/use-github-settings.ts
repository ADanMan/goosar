'use client';

import { useMemo } from 'react';
import { useCurrentWorkspace } from '../paths';
import { deriveGitHubSettings, type GitHubSettings } from './settings';

export function useGitHubSettings(): GitHubSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGitHubSettings(workspace), [workspace]);
}
