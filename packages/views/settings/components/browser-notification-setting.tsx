'use client';

import { useEffect, useState } from 'react';
import {
  getWebNotificationPermission,
  isWebNotificationSupported,
  requestWebNotificationPermission,
  type WebNotificationPermission,
} from '@goosar/core/platform';
import { Button } from '@goosar/ui/components/ui/button';
import { isDesktopShell } from '../../platform';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow } from './settings-layout';

export function BrowserNotificationSetting() {
  const { t } = useT('settings');
  const [mounted, setMounted] = useState(false);
  const [permission, setPermission] = useState<WebNotificationPermission>('default');

  useEffect(() => {
    setMounted(true);
    setPermission(getWebNotificationPermission());
  }, []);

  if (!mounted || isDesktopShell() || !isWebNotificationSupported()) return null;

  const handleEnable = async () => {
    setPermission(await requestWebNotificationPermission());
  };

  const statusHint =
    permission === 'granted'
      ? t(($) => $.notifications.browser.granted)
      : permission === 'denied'
        ? t(($) => $.notifications.browser.denied)
        : t(($) => $.notifications.browser.hint);

  return (
    <SettingsCard>
      <SettingsRow label={t(($) => $.notifications.browser.label)} description={statusHint}>
        {permission === 'default' && (
          <Button size="sm" variant="outline" onClick={handleEnable}>
            {t(($) => $.notifications.browser.enable)}
          </Button>
        )}
        {permission === 'granted' && (
          <span className="shrink-0 text-xs font-medium text-muted-foreground">
            {t(($) => $.notifications.browser.enabled_badge)}
          </span>
        )}
      </SettingsRow>
    </SettingsCard>
  );
}
