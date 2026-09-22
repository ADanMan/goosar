/**
 * Однострочное текстовое поле. `fontSize` задаётся инлайново, а не через
 * Tailwind `text-*` — на iOS `lineHeight` обрезает нижние выносные элементы
 * шрифта. Состояние фокуса отслеживается вручную, так как вариант `focus:`
 * у NativeWind для TextInput ведёт себя нестабильно.
 */
import { useState } from 'react';
import { TextInput, type TextInputProps } from 'react-native';
import { cn } from '@/lib/utils';
import { MOBILE_PLACEHOLDER_COLOR } from './input-tokens';

export interface TextFieldProps extends TextInputProps {
  className?: string;
  invalid?: boolean;
}

export function TextField({ className, style, invalid, onFocus, onBlur, ...rest }: TextFieldProps) {
  const [focused, setFocused] = useState(false);

  return (
    <TextInput
      placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
      style={[{ fontSize: 14, includeFontPadding: false, textAlignVertical: 'center' }, style]}
      onFocus={(e) => {
        setFocused(true);
        onFocus?.(e);
      }}
      onBlur={(e) => {
        setFocused(false);
        onBlur?.(e);
      }}
      className={cn(
        'rounded-md px-3 h-10 text-foreground border',
        invalid
          ? 'bg-destructive/10 border-destructive/60'
          : focused
            ? 'bg-secondary border-ring'
            : 'bg-secondary/50 border-transparent',
        className,
      )}
      {...rest}
    />
  );
}
