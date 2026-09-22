/**
 * Тулбар над клавиатурой для любого markdown-поля (описание задачи,
 * комментарий, промпт агента): @ · список · чекбокс · код · цитата ·
 * картинка · файл.
 *
 * Все кнопки вставляют markdown как текст — WYSIWYG не используется.
 * Кнопок форматирования (жирный/курсив/заголовок) нет — они требуют
 * стилизованного текста внутри TextInput, чего RN не умеет из коробки.
 */
import { Pressable, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Text } from '@/components/ui/text';
import { cn } from '@/lib/utils';

export interface MarkdownToolbarProps {
  onAt: () => void;
  onList: () => void;
  onCheckbox: () => void;
  onCode: () => void;
  onQuote: () => void;
  onImage?: () => void;
  onFile?: () => void;
  disabled?: boolean;
}

const ICON_COLOR = '#71717a'; 

export function MarkdownToolbar({
  onAt,
  onList,
  onCheckbox,
  onCode,
  onQuote,
  onImage,
  onFile,
  disabled,
}: MarkdownToolbarProps) {
  return (
    <View className="flex-row items-center gap-1 px-2 py-1.5 border-t border-border bg-background">
      <ToolbarButton accessibilityLabel="Mention someone" onPress={onAt} disabled={disabled}>
        <Text className="text-base text-muted-foreground leading-none">@</Text>
      </ToolbarButton>
      <ToolbarButton accessibilityLabel="Bullet list" onPress={onList} disabled={disabled}>
        <Ionicons name="list-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton accessibilityLabel="Checklist" onPress={onCheckbox} disabled={disabled}>
        <Ionicons name="checkbox-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton accessibilityLabel="Code block" onPress={onCode} disabled={disabled}>
        <Ionicons name="code-slash-outline" size={18} color={ICON_COLOR} />
      </ToolbarButton>
      <ToolbarButton accessibilityLabel="Quote" onPress={onQuote} disabled={disabled}>
        {/* Ionicons has no good quote glyph — use the literal " character at
         *   a slightly larger size for visual parity with adjacent icons. */}
        <Text className="text-xl text-muted-foreground leading-none -mt-1">&quot;</Text>
      </ToolbarButton>
      {onImage ? (
        <ToolbarButton accessibilityLabel="Attach image" onPress={onImage} disabled={disabled}>
          <Ionicons name="image-outline" size={18} color={ICON_COLOR} />
        </ToolbarButton>
      ) : null}
      {onFile ? (
        <ToolbarButton accessibilityLabel="Attach file" onPress={onFile} disabled={disabled}>
          <Ionicons name="attach-outline" size={18} color={ICON_COLOR} />
        </ToolbarButton>
      ) : null}
    </View>
  );
}

function ToolbarButton({
  onPress,
  disabled,
  accessibilityLabel,
  children,
}: {
  onPress: () => void;
  disabled?: boolean;
  accessibilityLabel: string;
  children: React.ReactNode;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      hitSlop={6}
      className={cn(
        'h-9 w-9 items-center justify-center rounded-md active:bg-secondary',
        disabled && 'opacity-40',
      )}
    >
      {children}
    </Pressable>
  );
}
