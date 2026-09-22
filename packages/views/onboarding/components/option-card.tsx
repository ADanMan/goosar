'use client';

import { Input } from '@goosar/ui/components/ui/input';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../../i18n';

const OTHER_INPUT_MAX_LENGTH = 80;

export function OptionCard({
  selected,
  onSelect,
  label,
}: {
  selected: boolean;
  onSelect: () => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        'group flex w-full items-center gap-3.5 rounded-lg border bg-card px-4 py-2.5 text-left transition-all',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20 hover:bg-accent/30',
      )}
    >
      <RadioMark selected={selected} />
      <span className="text-[14.5px] font-normal leading-tight text-foreground">{label}</span>
    </button>
  );
}

export function OtherOptionCard({
  selected,
  onSelect,
  otherValue,
  onOtherChange,
  placeholder,
}: {
  selected: boolean;
  onSelect: () => void;
  otherValue: string;
  onOtherChange: (value: string) => void;
  placeholder: string;
}) {
  const { t } = useT('onboarding');
  return (
    <div
      className={cn(
        'flex w-full flex-col rounded-lg border bg-card transition-all',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20',
      )}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        onClick={onSelect}
        className="flex w-full items-center gap-3.5 px-4 py-2.5 text-left"
      >
        <RadioMark selected={selected} />
        <span className="text-[14.5px] font-normal leading-tight text-foreground">
          {t(($) => $.option_card.other_label)}
        </span>
      </button>
      {selected && (
        <div className="px-4 pb-3 pl-[44px]">
          <Input
            autoFocus
            type="text"
            value={otherValue}
            onChange={(e) => onOtherChange(e.target.value)}
            placeholder={placeholder}
            maxLength={OTHER_INPUT_MAX_LENGTH}
            className="h-8 rounded-none border-x-0 border-t-0 border-b px-0 text-sm shadow-none focus-visible:border-foreground focus-visible:ring-0"
            aria-label={placeholder}
          />
        </div>
      )}
    </div>
  );
}

export function RadioMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        'relative inline-block h-4 w-4 shrink-0 rounded-full border-[1.5px] transition-colors',
        selected ? 'border-foreground' : 'border-border',
      )}
    >
      {selected && <span className="absolute inset-[3px] rounded-full bg-foreground" />}
    </span>
  );
}

export { OTHER_INPUT_MAX_LENGTH };
