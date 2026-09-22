'use client';

import type { Label } from '@goosar/core/types';
import { X } from 'lucide-react';
import { useT } from '../i18n';

function contrastTextColor(hex: string): string {
  const h = hex.replace('#', '');
  if (h.length !== 6) return '#111827';
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const luminance = 0.299 * r + 0.587 * g + 0.114 * b;
  return luminance > 0.55 ? '#111827' : '#f9fafb';
}

interface LabelChipProps {
  label: Label;
  onRemove?: () => void;
  className?: string;
  fullName?: boolean;
}

export function LabelChip({ label, onRemove, className, fullName }: LabelChipProps) {
  const { t } = useT('labels');
  const textColor = contrastTextColor(label.color);
  const nameClass = fullName ? 'break-all' : 'truncate max-w-[12rem]';
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${className ?? ''}`}
      style={{ backgroundColor: label.color, color: textColor }}
      aria-label={label.name}
      title={label.name}
    >
      <span className={nameClass}>{label.name}</span>
      {onRemove && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full hover:bg-current/20 focus:outline-none focus:ring-1 focus:ring-current"
          aria-label={t(($) => $.remove_label, { name: label.name })}
        >
          <X className="h-2.5 w-2.5" strokeWidth={2.5} />
        </button>
      )}
    </span>
  );
}
