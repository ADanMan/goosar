/**
 * Многострочное текстовое поле, растущее вместе с содержимым — заменяет
 * голый TextInput с tailwind min-h/max-h, который не растёт сам по себе.
 * Высота = clamp(contentSize.height, minHeight, maxHeight); при
 * достижении maxHeight включается внутренний скролл поля. Реф
 * прокидывается наружу для императивных focus()/blur().
 */

import * as React from 'react';
import { useState } from 'react';
import {
  TextInput,
  type NativeSyntheticEvent,
  type TextInputContentSizeChangeEventData,
  type TextInputProps,
} from 'react-native';
import { cn } from '@/lib/utils';
import { MOBILE_PLACEHOLDER_COLOR } from './input-tokens';

export interface AutosizeTextAreaProps extends TextInputProps {
  minHeight?: number;
  maxHeight?: number;
  className?: string;
}

export const AutosizeTextArea = React.forwardRef<TextInput, AutosizeTextAreaProps>(
  ({ minHeight = 40, maxHeight = 128, className, style, onContentSizeChange, ...rest }, ref) => {
    const [height, setHeight] = useState(minHeight);

    const handleContentSizeChange = (
      e: NativeSyntheticEvent<TextInputContentSizeChangeEventData>,
    ) => {
      const next = Math.min(Math.max(minHeight, e.nativeEvent.contentSize.height), maxHeight);
      setHeight(next);
      onContentSizeChange?.(e);
    };

    return (
      <TextInput
        ref={ref}
        multiline
        scrollEnabled={height >= maxHeight}
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        onContentSizeChange={handleContentSizeChange}
        style={[
          {
            height,
            paddingVertical: 0,
            includeFontPadding: false,
            textAlignVertical: 'top',
          },
          style,
        ]}
        className={cn('text-base text-foreground', className)}
        {...rest}
      />
    );
  },
);
AutosizeTextArea.displayName = 'AutosizeTextArea';
