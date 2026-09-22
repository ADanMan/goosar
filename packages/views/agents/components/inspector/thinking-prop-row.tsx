'use client';

import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import type { RuntimeModel } from '@goosar/core/types';
import { runtimeModelsOptions } from '@goosar/core/runtimes';
import { PropRow } from '../../../common/prop-row';
import { SettingsRow } from '../../../settings/components/settings-layout';
import { useT } from '../../../i18n';
import { ThinkingPicker } from './thinking-picker';

export function ThinkingPropRow({
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  canEdit,
  onChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  provider: string;
  model: string;
  value: string;
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT('agents');
  const modelsQuery = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));

  const models = modelsQuery.data?.models ?? [];
  const entry = pickModelEntry(models, model, provider);
  const levels = entry?.thinking?.supported_levels ?? [];
  if (levels.length === 0 && !value) return null;

  return (
    <PropRow label={t(($) => $.inspector.prop_thinking)} interactive={false}>
      <ThinkingPicker value={value} levels={levels} canEdit={canEdit} onChange={onChange} />
    </PropRow>
  );
}

export function ThinkingSettingField({
  label,
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  canEdit,
  onChange,
}: {
  label: ReactNode;
  runtimeId: string | null;
  runtimeOnline: boolean;
  provider: string;
  model: string;
  value: string;
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const modelsQuery = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));
  const models = modelsQuery.data?.models ?? [];
  const entry = pickModelEntry(models, model, provider);
  const levels = entry?.thinking?.supported_levels ?? [];

  if (levels.length === 0 && !value) return null;

  return (
    <SettingsRow label={label} size="select-wide">
      <ThinkingPicker
        variant="field"
        showLabel={false}
        value={value}
        levels={levels}
        canEdit={canEdit}
        onChange={onChange}
      />
    </SettingsRow>
  );
}

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
  provider: string,
): RuntimeModel | undefined {
  if (model) return models.find((m) => m.id === model);
  if (provider === 'runtime-e') return undefined;
  return models.find((m) => m.default) ?? models[0];
}
