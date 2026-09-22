'use client';

import { useMemo, useState } from 'react';
import { Check, ChevronDown, Globe } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { PropertyPicker, PickerEmpty } from '../../../issues/components/pickers/property-picker';
import { useT } from '../../../i18n';

export interface TimezonePickerProps {
  value: string;
  onChange: (tz: string) => void;
  options: string[];
  disabled?: boolean;
  className?: string;
  ariaLabel?: string;
}

const OFFSET_TTL_MS = 10 * 60_000;
const offsetCache = new Map<string, { offset: string; at: number }>();

function offsetFor(tz: string): string {
  const now = Date.now();
  const cached = offsetCache.get(tz);
  if (cached !== undefined && now - cached.at < OFFSET_TTL_MS) return cached.offset;
  const offset = computeOffset(tz);
  offsetCache.set(tz, { offset, at: now });
  return offset;
}

function computeOffset(tz: string): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      timeZoneName: 'shortOffset',
    }).formatToParts(new Date());
    return parts.find((p) => p.type === 'timeZoneName')?.value ?? '';
  } catch {
    return '';
  }
}

function cityLabel(tz: string): string {
  if (tz === 'UTC') return 'UTC';
  return tz.split('/').pop()?.replace(/_/g, ' ') ?? tz;
}

export function TimezonePicker({
  value,
  onChange,
  options,
  disabled,
  className,
  ariaLabel,
}: TimezonePickerProps) {
  const { t } = useT('autopilots');
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');

  const selectedCity = cityLabel(value);
  const selectedOffset = offsetFor(value);

  const query = filter.trim().toLowerCase();
  const filteredOptions = useMemo(() => {
    if (!query) return options;
    return options.filter((tz) => {
      const haystack = `${tz} ${cityLabel(tz)} ${offsetFor(tz)}`.toLowerCase();
      return haystack.includes(query);
    });
  }, [options, query]);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setFilter('');
      }}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.timezone_picker.search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={
        <button
          type="button"
          disabled={disabled}
          aria-label={
            ariaLabel === undefined
              ? undefined
              : `${ariaLabel}: ${[selectedCity, selectedOffset].filter(Boolean).join(' ')}`
          }
          className={cn(
            'flex h-8 w-full items-center gap-1.5 rounded-lg border border-input bg-transparent px-2.5 text-sm transition-colors outline-none',
            'hover:bg-accent/30',
            'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50',
            'disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50',
            'dark:bg-input/30',
            className,
          )}
        />
      }
      trigger={
        <>
          <Globe className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="flex-1 truncate text-left">{selectedCity}</span>
          {selectedOffset && (
            <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
              {selectedOffset}
            </span>
          )}
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {/* Built only while the popover is open: each row's offset lookup can cold-
          build an Intl.DateTimeFormat, and evaluating the full IANA list here on
          every render would pay that burst on dialog open (and again on each
          cache expiry) for a list the user may never look at. */}
      {!open ? null : filteredOptions.length === 0 ? (
        <PickerEmpty />
      ) : (
        filteredOptions.map((tz) => {
          const off = offsetFor(tz);
          const isSelected = tz === value;
          return (
            <button
              key={tz}
              type="button"
              data-picker-item
              onClick={() => {
                onChange(tz);
                setOpen(false);
              }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
            >
              <span className="flex size-3.5 shrink-0 items-center justify-center">
                {isSelected && <Check className="size-3.5 text-foreground" />}
              </span>
              <span className="flex-1 truncate text-left">{cityLabel(tz)}</span>
              {off && (
                <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{off}</span>
              )}
            </button>
          );
        })
      )}
    </PropertyPicker>
  );
}
