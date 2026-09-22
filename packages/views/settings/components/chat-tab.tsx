'use client';

import { Switch } from '@goosar/ui/components/ui/switch';
import { useChatStore } from '@goosar/core/chat';
import { toast } from 'sonner';
import { useT } from '../../i18n';
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from './settings-layout';

export function ChatTab() {
  const { t } = useT('settings');
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const setEnabled = useChatStore((s) => s.setFloatingChatEnabled);

  return (
    <SettingsTab title={t(($) => $.page.tabs.chat)}>
      <SettingsSection title={t(($) => $.chat.floating_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.chat.floating_label)}
            description={t(($) => $.chat.floating_hint)}
          >
            <Switch
              checked={enabled}
              onCheckedChange={(checked) => {
                setEnabled(checked);
                toast.success(
                  t(($) => $.auto_save.toast_saved),
                  {
                    id: 'settings-auto-save',
                  },
                );
              }}
              aria-label={t(($) => $.chat.floating_label)}
            />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
