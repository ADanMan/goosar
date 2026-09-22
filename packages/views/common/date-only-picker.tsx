'use client';

import { useState } from 'react';
import {
  toDateOnly,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
} from '@goosar/core/issues/date';
import { Calendar } from '@goosar/ui/components/ui/calendar';
import { Popover, PopoverTrigger, PopoverContent } from '@goosar/ui/components/ui/popover';
import { Button } from '@goosar/ui/components/ui/button';
import { useUiLocale } from '../i18n';
import { DeferredPopup } from './deferred-popup';

const DATE_TRIGGER_CLASS =
  'flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors';

interface DateOnlyPickerProps {
  value: string | null;
  onChange: (value: string | null) => void;
  icon: React.ReactNode;
  placeholder: string;
  clearLabel: string;
  highlightOverdue?: boolean;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: 'start' | 'center' | 'end';
  defaultOpen?: boolean;
}

export function DateOnlyPicker(props: DateOnlyPickerProps) {
  const canDefer =
    props.open === undefined && props.onOpenChange === undefined && !props.defaultOpen;
  if (!canDefer) {
    return <DateOnlyPickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger ?? <DateTriggerContent {...props} />}
      triggerRender={props.triggerRender}
      triggerClassName={DATE_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <DateOnlyPickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function DateTriggerContent({
  value,
  icon,
  placeholder,
  highlightOverdue = false,
}: Pick<DateOnlyPickerProps, 'value' | 'icon' | 'placeholder' | 'highlightOverdue'>) {
  const date = dateOnlyToLocalDate(value);
  const overdue = highlightOverdue && isPastDateOnly(value);
  return (
    <>
      {icon}
      {date ? (
        <span className={overdue ? 'text-destructive' : ''}>
          {formatDateOnly(value, { month: 'short', day: 'numeric' }, 'en-US')}
        </span>
      ) : (
        <span className="text-muted-foreground">{placeholder}</span>
      )}
    </>
  );
}

function DateOnlyPickerImpl({
  value,
  onChange,
  icon,
  placeholder,
  clearLabel,
  highlightOverdue = false,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align = 'start',
  defaultOpen = false,
}: DateOnlyPickerProps) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const uiLocale = useUiLocale();
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const date = dateOnlyToLocalDate(value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className={triggerRender ? undefined : DATE_TRIGGER_CLASS}
        render={triggerRender}
      >
        {customTrigger ?? (
          <DateTriggerContent
            value={value}
            icon={icon}
            placeholder={placeholder}
            highlightOverdue={highlightOverdue}
          />
        )}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <Calendar
          mode="single"
          locale={uiLocale}
          selected={date}
          onSelect={(d: Date | undefined) => {
            onChange(d ? toDateOnly(d) : null);
            setOpen(false);
          }}
        />
        {date && (
          <div className="border-t px-3 py-2">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                onChange(null);
                setOpen(false);
              }}
              className="text-muted-foreground hover:text-foreground"
            >
              {clearLabel}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
