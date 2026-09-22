/**
 * Блок ввода описания, общий для new-issue.tsx и issue/[id]/edit.tsx.
 * Контейнер с подсветкой фокуса вокруг AutosizeTextArea — тот же стиль,
 * что и у композера комментариев. Чистая UI-обёртка: пайплайн mention
 * живёт в переданном вызывающей стороной useMentionInput, а плавающий
 * MentionSuggestionBar тоже остаётся на стороне вызывающего.
 */
import { useState } from 'react';
import { View } from 'react-native';
import { AutosizeTextArea } from '@/components/ui/autosize-textarea';
import { MIN_BODY_INPUT_HEIGHT_PX } from '@/components/ui/input-tokens';
import { cn } from '@/lib/utils';
import type { UseMentionInputReturn } from '@/lib/use-mention-input';

export function DescriptionField({
  description,
  disabled,
  placeholder = 'Description… (type @ to mention)',
}: {
  description: UseMentionInputReturn;
  disabled: boolean;
  placeholder?: string;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <View
      className={cn(
        'rounded-2xl border px-3',
        focused ? 'border-primary/30 bg-secondary' : 'border-transparent bg-secondary/40',
      )}
    >
      <AutosizeTextArea
        value={description.text}
        onChangeText={description.handlers.onChangeText}
        selection={description.selection}
        onSelectionChange={description.handlers.onSelectionChange}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder={placeholder}
        className="py-2"
        minHeight={MIN_BODY_INPUT_HEIGHT_PX}
        editable={!disabled}
      />
    </View>
  );
}
