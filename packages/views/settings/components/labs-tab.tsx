'use client';

import { FlaskConical } from 'lucide-react';
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@goosar/ui/components/ui/empty';
import { useT } from '../../i18n';
import { SettingsCard, SettingsTab } from './settings-layout';

export function LabsTab() {
  const { t } = useT('settings');
  return (
    <SettingsTab title={t(($) => $.page.tabs.labs)}>
      <SettingsCard>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FlaskConical className="h-4 w-4" />
            </EmptyMedia>
            <EmptyTitle>{t(($) => $.labs.section_placeholder_title)}</EmptyTitle>
            <EmptyDescription>{t(($) => $.labs.section_placeholder_description)}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </SettingsCard>
    </SettingsTab>
  );
}
