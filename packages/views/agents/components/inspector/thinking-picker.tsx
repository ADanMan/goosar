'use client';

import { useState } from 'react';
import { Brain, ChevronDown } from 'lucide-react';
import type { RuntimeModelThinkingLevel } from '@goosar/core/types';
import { Label } from '@goosar/ui/components/ui/label';
import { PickerItem, PropertyPicker } from '../../../issues/components/pickers';
import { CHIP_CLASS } from './chip';
import { useT } from '../../../i18n';

export function ThinkingPicker({
  value,
  levels,
  canEdit = true,
  variant = 'chip',
  showLabel = true,
  onChange,
}: {
  value: string;
  levels: RuntimeModelThinkingLevel[];
  canEdit?: boolean;
  variant?: 'chip' | 'field';
  showLabel?: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT('agents');
  const [open, setOpen] = useState(false);

  const selected = value ? levels.find((l) => l.value === value) : undefined;
  const triggerLabel = selected ? selected.label : value || t(($) => $.pickers.thinking_default);
  const triggerTitle = t(($) => $.pickers.thinking_tooltip, {
    value: triggerLabel,
  });

  const select = async (next: string) => {
    setOpen(false);
    if (next !== value) await onChange(next);
  };

  if (!canEdit) {
    if (variant === 'field') {
      const control = (
        <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-sm text-muted-foreground">
          <Brain className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="min-w-0 truncate">{triggerLabel}</span>
        </div>
      );
      if (!showLabel) return control;
      return (
        <div className="flex min-w-0 flex-col">
          <Label>{t(($) => $.inspector.prop_thinking)}</Label>
          <div className="mt-1.5">{control}</div>
        </div>
      );
    }
    return (
      <span
        className="min-w-0 truncate px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
        title={triggerTitle}
      >
        {triggerLabel}
      </span>
    );
  }

  const picker = (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width={
        variant === 'field'
          ? 'w-[var(--anchor-width)] min-w-[14rem] max-w-md'
          : 'w-auto min-w-[14rem] max-w-md'
      }
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className={
            variant === 'field'
              ? `${showLabel ? 'mt-1.5 ' : ''}flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-sm transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50`
              : CHIP_CLASS
          }
          aria-label={triggerTitle}
        />
      }
      trigger={
        <>
          {variant === 'field' ? (
            <Brain className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          ) : null}
          <span
            className={
              variant === 'field'
                ? 'min-w-0 flex-1 truncate'
                : 'min-w-0 truncate font-mono text-[11px]'
            }
          >
            {triggerLabel}
          </span>
          {variant === 'field' ? (
            <ChevronDown
              className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
                open ? 'rotate-180' : ''
              }`}
              aria-hidden="true"
            />
          ) : null}
        </>
      }
    >
      {levels.map((l) => (
        <PickerItem key={l.value} selected={l.value === value} onClick={() => void select(l.value)}>
          {/* PickerItem wraps children in a flex `<span>`. Putting a
              `<div>` inside that <span> is block-in-inline (invalid HTML5)
              and triggers browser quirks that shift descendant x-position.
              Use a `<span>` with explicit `block` + `text-left` so layout
              is deterministic across rows regardless of whether the label
              row has the `default` badge sibling. */}
          {/* No model-factory-default badge here on purpose: when the
              picker is "Follow CLI config" (value === ""), Goosar omits
              `--effort` and the local CLI config decides — the model's
              factory default is irrelevant to what actually fires, so
              flagging one option as "default" was misleading. */}
          <span className="block min-w-0 flex-1 text-left">
            <span className="truncate text-[13px] font-medium">{l.label}</span>
            {l.description && (
              <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                {l.description}
              </span>
            )}
          </span>
        </PickerItem>
      ))}

      {value && (
        <button
          type="button"
          onClick={() => void select('')}
          className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
          title={t(($) => $.pickers.thinking_clear_title)}
        >
          {t(($) => $.pickers.thinking_clear)}
        </button>
      )}
    </PropertyPicker>
  );

  if (variant === 'field') {
    if (!showLabel) return picker;
    return (
      <div className="flex min-w-0 flex-col">
        <Label>{t(($) => $.inspector.prop_thinking)}</Label>
        {picker}
      </div>
    );
  }

  return picker;
}
