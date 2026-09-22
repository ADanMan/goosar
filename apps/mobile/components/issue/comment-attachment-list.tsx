/**
 * Отдельный список вложений для карточки комментария: показывает
 * вложения, на которые нет ссылки внутри markdown-текста, с дедупом
 * по файлу.
 *
 * Комментарии с мобильного всегда несут вложения через поле attachments
 * (без инлайновых ![]() в тексте), поэтому список нужен именно там.
 * Комментарии с веба с уже инлайновыми картинками рендерятся через
 * MarkdownImage, и список возвращает null — показывать нечего.
 */
import { useMemo } from 'react';
import { Linking, Pressable, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import type { Attachment } from '@goosar/core/types';
import { standaloneAttachments } from '@/lib/attachment-dedup';
import { MarkdownImage } from '@/lib/markdown/markdown-image';
import { resolveAttachmentUrl } from '@/lib/attachment-url';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { Text } from '@/components/ui/text';

interface Props {
  attachments?: Attachment[];
  content?: string;
}

export function CommentAttachmentList({ attachments, content }: Props) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const standalone = useMemo(
    () => standaloneAttachments(attachments, content),
    [attachments, content],
  );

  if (standalone.length === 0) return null;

  return (
    <View className="gap-1.5">
      {standalone.map((attachment) => {
        const isImage = attachment.content_type.startsWith('image/');
        if (isImage) {
          return (
            <MarkdownImage
              key={attachment.id}
              uri={attachment.url}
              alt={attachment.filename}
              attachments={attachments}
            />
          );
        }
        return <FileCard key={attachment.id} attachment={attachment} theme={theme} />;
      })}
    </View>
  );
}

function FileCard({
  attachment,
  theme,
}: {
  attachment: Attachment;
  theme: (typeof THEME)['light'];
}) {
  const sizeLabel = formatBytes(attachment.size_bytes);
  return (
    <Pressable
      onPress={() => {
        const target = resolveAttachmentUrl(attachment.download_url);
        if (target) {
          void Linking.openURL(target);
        }
      }}
      accessibilityRole="button"
      accessibilityLabel={`Open ${attachment.filename}`}
      className="flex-row items-center gap-2 px-3 py-2 rounded-md bg-secondary/60 active:opacity-80"
    >
      <Ionicons name="document-outline" size={20} color={theme.mutedForeground} />
      <View className="flex-1">
        <Text className="text-sm text-foreground" numberOfLines={1}>
          {attachment.filename}
        </Text>
        {sizeLabel ? <Text className="text-xs text-muted-foreground">{sizeLabel}</Text> : null}
      </View>
      <Ionicons name="download-outline" size={18} color={theme.mutedForeground} />
    </Pressable>
  );
}

function formatBytes(bytes: number): string | null {
  if (!bytes || bytes <= 0) return null;
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex++;
  }
  const formatted = value < 10 ? value.toFixed(1) : Math.round(value).toString();
  return `${formatted} ${units[unitIndex]}`;
}
