'use client';

import { useEffect, useRef } from 'react';
import {
  registerSystemNotificationClickHandler,
  type SystemNotificationPayload,
} from '@goosar/core/platform';
import { paths } from '@goosar/core/paths';
import { useNavigation } from '@goosar/views/navigation';

export function WebNotificationBridge() {
  const { push } = useNavigation();
  const pushRef = useRef(push);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);

  useEffect(() => {
    registerSystemNotificationClickHandler(({ slug, issueKey }: SystemNotificationPayload) => {
      if (!slug) return;
      const inboxPath = `${paths.workspace(slug).inbox()}?issue=${encodeURIComponent(issueKey)}`;
      pushRef.current(inboxPath);
    });
    return () => registerSystemNotificationClickHandler(null);
  }, []);

  return null;
}
