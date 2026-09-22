/**
 * Единый ряд чипов для вложений в композере комментария: mention (@имя,
 * тап только удаляет), image (превью по локальному file:// uri, чтобы не
 * ждать серверный URL) и file (тап открывает download_url в Safari после
 * загрузки). Капсула вместо превью-картинки — так ряд остаётся сводкой
 * «что прикреплено», а не визуальной галереей.
 */
import { useMemo } from 'react';
import { ActivityIndicator, Linking, Pressable, ScrollView, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { resolveAttachmentUrl } from '@/lib/attachment-url';
import { useLightbox } from '@/lib/markdown/lightbox-provider';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { Text } from '@/components/ui/text';

export type MentionChipType = 'member' | 'agent' | 'squad' | 'all' | 'issue';

export interface MentionChip {
  type: MentionChipType;
  id: string;
  name: string;
}

export type ComposerAttachmentStatus = 'uploading' | 'completed' | 'failed';

export interface ComposerAttachmentItem {
  localId: string;
  localUri: string;
  filename: string;
  mimeType: string;
  status: ComposerAttachmentStatus;
  id?: string;
  url?: string;
  downloadUrl?: string;
  error?: string;
}

interface Props {
  mentions: MentionChip[];
  attachments: ComposerAttachmentItem[];
  onRemoveMention: (type: MentionChipType, id: string) => void;
  onRemoveAttachment: (localId: string) => void;
  onRetryAttachment?: (localId: string) => void;
}

export function ComposerAttachmentRow({
  mentions,
  attachments,
  onRemoveMention,
  onRemoveAttachment,
  onRetryAttachment,
}: Props) {
  if (mentions.length === 0 && attachments.length === 0) return null;

  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={{ gap: 6, paddingHorizontal: 2, paddingVertical: 2 }}
      keyboardShouldPersistTaps="handled"
    >
      {mentions.map((m) => (
        <MentionChipView key={`m:${m.type}:${m.id}`} mention={m} onRemove={onRemoveMention} />
      ))}
      {attachments.map((a) => (
        <AttachmentChipView
          key={a.localId}
          item={a}
          onRemove={onRemoveAttachment}
          onRetry={onRetryAttachment}
        />
      ))}
    </ScrollView>
  );
}

function MentionChipView({
  mention,
  onRemove,
}: {
  mention: MentionChip;
  onRemove: (type: MentionChipType, id: string) => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const iconName =
    mention.type === 'all' ? 'people' : mention.type === 'issue' ? 'git-branch-outline' : 'person';

  const label = mention.type === 'issue' ? mention.name : `@${mention.name}`;

  return (
    <View className="flex-row items-center gap-1 h-7 px-2 rounded-full bg-primary/10">
      <Ionicons name={iconName} size={12} color={theme.primary} />
      <Text className="text-xs font-medium text-foreground">{label}</Text>
      <Pressable
        onPress={() => onRemove(mention.type, mention.id)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Remove mention ${mention.name}`}
        className="h-4 w-4 items-center justify-center"
      >
        <Ionicons name="close" size={12} color={theme.mutedForeground} />
      </Pressable>
    </View>
  );
}

interface AttachmentChipProps {
  item: ComposerAttachmentItem;
  onRemove: (localId: string) => void;
  onRetry?: (localId: string) => void;
}

function AttachmentChipView({ item, onRemove, onRetry }: AttachmentChipProps) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { open } = useLightbox();

  const isImage = useMemo(() => item.mimeType.startsWith('image/'), [item.mimeType]);

  const onPress = () => {
    if (item.status === 'failed' && onRetry) {
      onRetry(item.localId);
      return;
    }
    if (item.status !== 'completed') return;
    if (isImage) {
      open(item.localUri);
    } else {
      const target = resolveAttachmentUrl(item.downloadUrl);
      if (target) void Linking.openURL(target);
    }
  };

  const iconName =
    item.status === 'failed' ? 'refresh' : isImage ? 'image-outline' : 'document-outline';

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole={item.status === 'failed' ? 'button' : 'image'}
      accessibilityLabel={
        item.status === 'failed' ? `Retry upload of ${item.filename}` : `Open ${item.filename}`
      }
      className="flex-row items-center gap-1 h-7 px-2 rounded-full bg-secondary active:opacity-80"
    >
      {item.status === 'uploading' ? (
        <ActivityIndicator size="small" color={theme.mutedForeground} />
      ) : (
        <Ionicons
          name={iconName}
          size={12}
          color={item.status === 'failed' ? theme.destructive : theme.mutedForeground}
        />
      )}
      <Text className="text-xs text-foreground max-w-[120px]" numberOfLines={1}>
        {item.filename}
      </Text>
      <Pressable
        onPress={() => onRemove(item.localId)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Remove ${item.filename}`}
        className="h-4 w-4 items-center justify-center"
      >
        <Ionicons name="close" size={12} color={theme.mutedForeground} />
      </Pressable>
    </Pressable>
  );
}
