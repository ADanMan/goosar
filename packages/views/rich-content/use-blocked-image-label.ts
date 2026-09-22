'use client';

import { useEffect } from 'react';
import { setBlockedImageLabels } from '@goosar/ui/markdown';
import { useT } from '../i18n';

export function useSyncBlockedImageLabel(): void {
  const { t } = useT('editor');

  useEffect(() => {
    setBlockedImageLabels({
      ariaLabel: t(($) => $.blocked_image.aria_label),
      text: t(($) => $.blocked_image.text),
      title: t(($) => $.blocked_image.title),
    });
  }, [t]);
}
