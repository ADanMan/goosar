'use client';

import { useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Tag, Plus, Settings2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Label } from '@goosar/core/types';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useWorkspacePaths } from '@goosar/core/paths';
import {
  labelListOptions,
  issueLabelsOptions,
  useAttachLabel,
  useDetachLabel,
  useCreateLabel,
} from '@goosar/core/labels';
import { LabelChip } from '../../../labels/label-chip';
import { useNavigation } from '../../../navigation';
import { PropertyPicker, PickerItem, PickerEmpty } from './property-picker';
import { useT } from '../../../i18n';

interface LabelPickerProps {
  issueId?: string;
  selectedIds?: string[];
  onSelectedIdsChange?: (ids: string[]) => void;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: 'start' | 'center' | 'end';
  defaultOpen?: boolean;
  triggerRender?: React.ReactElement;
}

const INLINE_COLORS = [
  '#ef4444',
  '#f97316',
  '#eab308',
  '#22c55e',
  '#14b8a6',
  '#3b82f6',
  '#6366f1',
  '#a855f7',
  '#ec4899',
  '#64748b',
] as const;

function pickInlineColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return INLINE_COLORS[hash % INLINE_COLORS.length] ?? INLINE_COLORS[0]!;
}

export function LabelPicker({
  issueId,
  selectedIds = [],
  onSelectedIdsChange,
  open: controlledOpen,
  onOpenChange,
  align = 'start',
  defaultOpen = false,
  triggerRender,
}: LabelPickerProps) {
  const { t } = useT('issues');
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState('');
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  const creatingRef = useRef(false);

  const isDraft = issueId === undefined;

  const wsId = useWorkspaceId();
  const { data: allLabels = [] } = useQuery(labelListOptions(wsId));
  const { data: attachedLabels = [] } = useQuery(issueLabelsOptions(wsId, issueId ?? ''));

  const attach = useAttachLabel(issueId ?? '');
  const detach = useDetachLabel(issueId ?? '');
  const create = useCreateLabel();

  const selectedLabels = useMemo<Label[]>(() => {
    if (!isDraft) return attachedLabels;
    return selectedIds
      .map((id) => allLabels.find((l) => l.id === id))
      .filter((l): l is Label => Boolean(l));
  }, [isDraft, attachedLabels, selectedIds, allLabels]);

  const selectedIdSet = useMemo(() => new Set(selectedLabels.map((l) => l.id)), [selectedLabels]);

  const query = filter.trim();
  const queryLower = query.toLowerCase();
  const filtered = allLabels.filter((l) => l.name.toLowerCase().includes(queryLower));
  const exactMatch = allLabels.some((l) => l.name.toLowerCase() === queryLower);
  const canCreate = query.length > 0 && !exactMatch && !create.isPending;

  const removeLabel = (labelId: string) => {
    if (isDraft) {
      onSelectedIdsChange?.(selectedIds.filter((id) => id !== labelId));
    } else {
      detach.mutate(labelId);
    }
  };

  const toggle = (labelId: string) => {
    if (isDraft) {
      onSelectedIdsChange?.(
        selectedIdSet.has(labelId)
          ? selectedIds.filter((id) => id !== labelId)
          : [...selectedIds, labelId],
      );
    } else if (selectedIdSet.has(labelId)) {
      detach.mutate(labelId);
    } else {
      attach.mutate(labelId);
    }
  };

  const createAndAttach = () => {
    if (!canCreate || creatingRef.current) return;
    creatingRef.current = true;
    const name = query;
    create.mutate(
      { name, color: pickInlineColor(name) },
      {
        onSuccess: (label) => {
          if (isDraft) {
            onSelectedIdsChange?.([...selectedIds, label.id]);
          } else {
            attach.mutate(label.id);
          }
          setFilter('');
        },
        onError: (err: unknown) => {
          toast.error(err instanceof Error ? err.message : t(($) => $.pickers.label.create_failed));
        },
        onSettled: () => {
          creatingRef.current = false;
        },
      },
    );
  };

  const openManage = () => {
    setOpen(false);
    navigation.push(`${paths.settings()}?tab=labels`);
  };

  const hasLabels = selectedLabels.length > 0;

  const resolvedTriggerRender =
    triggerRender ??
    (hasLabels ? (
      <div className="flex flex-wrap items-center gap-1 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors" />
    ) : undefined);

  return (
    <div className="flex flex-col gap-1.5">
      <PropertyPicker
        open={open}
        onOpenChange={(v: boolean) => {
          setOpen(v);
          if (!v) setFilter('');
        }}
        width="w-80"
        align={align}
        searchable
        searchPlaceholder={t(($) => $.pickers.label.search_placeholder)}
        onSearchChange={setFilter}
        triggerRender={resolvedTriggerRender}
        trigger={
          hasLabels ? (
            <>
              {selectedLabels.map((l) => (
                <LabelChip
                  key={l.id}
                  label={l}
                  onRemove={triggerRender ? undefined : () => removeLabel(l.id)}
                />
              ))}
            </>
          ) : (
            <>
              <Tag className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">
                {t(($) => $.pickers.label.trigger_label)}
              </span>
            </>
          )
        }
        footer={
          <button
            type="button"
            onClick={openManage}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-accent transition-colors"
          >
            <Settings2 className="h-3.5 w-3.5" />
            <span>{t(($) => $.pickers.label.manage_action)}</span>
          </button>
        }
      >
        {filtered.map((label) => {
          const selected = selectedIdSet.has(label.id);
          return (
            <PickerItem key={label.id} selected={selected} onClick={() => toggle(label.id)}>
              <span
                className="inline-block h-3 w-3 shrink-0 rounded-full"
                style={{ backgroundColor: label.color }}
                aria-hidden
              />
              <span className="truncate">{label.name}</span>
            </PickerItem>
          );
        })}
        {filtered.length === 0 && !canCreate && <PickerEmpty />}
        {canCreate && (
          <PickerItem selected={false} onClick={createAndAttach}>
            <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">
              {t(($) => $.pickers.label.create_action)}{' '}
              <span className="font-medium">&ldquo;{query}&rdquo;</span>
            </span>
            <span
              className="ml-auto inline-block h-3 w-3 shrink-0 rounded-full"
              style={{ backgroundColor: pickInlineColor(query) }}
              aria-hidden
            />
          </PickerItem>
        )}
      </PropertyPicker>
    </div>
  );
}
