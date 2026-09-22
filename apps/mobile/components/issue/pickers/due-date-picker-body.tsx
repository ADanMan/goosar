/**
 * Тело пикера срока выполнения — обёртка над нативным UIDatePicker.
 * Кнопки Done/Clear рисует вызывающий формSheet-маршрут в своей шапке,
 * этот компонент отвечает только за спиннер и локальный черновик даты.
 * due_date — календарный день без времени и часового пояса.
 */
import { useState, useEffect, useImperativeHandle, forwardRef } from 'react';
import { View } from 'react-native';
import DateTimePicker from '@react-native-community/datetimepicker';
import { toDateOnly, dateOnlyToLocalDate } from '@goosar/core/issues/date';

interface Props {
  value: string | null;
}

export interface DueDatePickerBodyHandle {
  getIso: () => string;
}

function toLocalDay(value: string | null): Date {
  return dateOnlyToLocalDate(value) ?? new Date();
}

export const DueDatePickerBody = forwardRef<DueDatePickerBodyHandle, Props>(
  function DueDatePickerBody({ value }, ref) {
    const [draft, setDraft] = useState<Date>(() => toLocalDay(value));

    useEffect(() => {
      setDraft(toLocalDay(value));
    }, [value]);

    useImperativeHandle(ref, () => ({
      getIso: () => toDateOnly(draft),
    }));

    return (
      <View className="flex-1 items-center pt-2">
        <DateTimePicker
          value={draft}
          mode="date"
          display="inline"
          onChange={(_event, selected) => {
            if (selected) setDraft(selected);
          }}
        />
      </View>
    );
  },
);
