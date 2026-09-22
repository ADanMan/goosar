'use client';

import { useEffect } from 'react';
import { useConfigStore } from '@goosar/core/config';
import { setMarkdownImagePolicy, useMarkdownImagePolicyVersion } from '@goosar/ui/markdown';
import { useAppOrigin } from '../navigation';

export function useSyncMarkdownImagePolicy(): number {
  const externalImages = useConfigStore((s) => s.externalImages);
  const imageHosts = useConfigStore((s) => s.imageHosts);
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const appOrigin = useAppOrigin();

  useEffect(() => {
    setMarkdownImagePolicy({
      mode: externalImages ?? 'allow',
      hosts: [...(imageHosts ?? []), cdnDomain ?? '', appOrigin ?? ''],
    });
  }, [externalImages, imageHosts, cdnDomain, appOrigin]);

  return useMarkdownImagePolicyVersion();
}
