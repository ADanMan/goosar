/**
 * Общий композер сообщений для треда комментариев к задаче и вкладки
 * чата. Два состояния: свёрнутое (кнопка-пилюля) и развёрнутое (чип
 * ответа → строка чипов с упоминаниями/файлами → поле ввода → тулбар).
 *
 * Упоминания вставляются как markdown-ссылки при отправке; вложения
 * передаются отдельным списком attachmentIds. Пикер упоминаний — это
 * отдельный formSheet-роут, обменивающийся данными через общий стор.
 */
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Alert, Keyboard, Pressable, TextInput, View } from 'react-native';
import { KeyboardStickyView } from 'react-native-keyboard-controller';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Ionicons } from '@expo/vector-icons';
import { router, type Href } from 'expo-router';
import * as Haptics from 'expo-haptics';
import * as ImagePicker from 'expo-image-picker';
import * as DocumentPicker from 'expo-document-picker';
import { api, MAX_FILE_SIZE } from '@/data/api';
import { useMentionDraftStore } from '@/data/stores/mention-draft-store';
import { useColorScheme } from '@/lib/use-color-scheme';
import { stripMarkdown } from '@/lib/strip-markdown';
import { THEME } from '@/lib/theme';
import { Text } from '@/components/ui/text';
import { IconButton } from '@/components/ui/icon-button';
import {
  ComposerAttachmentRow,
  type ComposerAttachmentItem,
  type MentionChip,
} from '@/components/issue/composer-attachment-row';

export interface MessageComposerReplyTarget {
  actorName: string;
  preview: string;
}

interface Props {
  onSubmit: (args: {
    content: string;
    attachmentIds: string[];
    mentions: MentionChip[];
  }) => Promise<void>;

  mentionPickerPath: Href;

  uploadContext?: { issueId?: string; commentId?: string };

  placeholder?: string;
  pillLabel?: string;
  pillIcon?: keyof typeof Ionicons.glyphMap;

  value?: string;
  onChangeText?: (next: string) => void;

  replyTarget?: MessageComposerReplyTarget | null;
  onClearReplyTarget?: () => void;

  expandTrigger?: string | null;

  isSending?: boolean;
  renderStop?: () => ReactNode;

  disabled?: boolean;
  disabledReason?: string;

  manageKeyboard?: boolean;
}

function makeLocalId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function serializeMentions(chips: MentionChip[]): string {
  return chips
    .map((m) => {
      const label = m.type === 'issue' ? m.name : m.type === 'all' ? '@all' : `@${m.name}`;
      return `[${label}](mention://${m.type}/${m.id})`;
    })
    .join(' ');
}

export function MessageComposer({
  onSubmit,
  mentionPickerPath,
  uploadContext,
  placeholder = 'Type a message…',
  pillLabel = 'Type a message…',
  pillIcon = 'chatbubble-ellipses-outline',
  value: controlledValue,
  onChangeText: controlledOnChange,
  replyTarget = null,
  onClearReplyTarget,
  expandTrigger,
  isSending = false,
  renderStop,
  disabled = false,
  disabledReason,
  manageKeyboard = true,
}: Props) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const insets = useSafeAreaInsets();
  const inputRef = useRef<TextInput>(null);
  const [expanded, setExpanded] = useState(false);
  const [internalText, setInternalText] = useState('');
  const [attachments, setAttachments] = useState<ComposerAttachmentItem[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const isControlled = controlledValue !== undefined && controlledOnChange !== undefined;
  const text = isControlled ? controlledValue : internalText;
  const setText = useCallback(
    (next: string) => {
      if (isControlled) {
        controlledOnChange(next);
      } else {
        setInternalText(next);
      }
    },
    [isControlled, controlledOnChange],
  );

  const mentions = useMentionDraftStore((s) => s.mentions);
  const removeMention = useMentionDraftStore((s) => s.remove);
  const clearMentions = useMentionDraftStore((s) => s.clear);

  useEffect(() => {
    return () => {
      clearMentions();
    };
  }, [clearMentions]);

  const triggerSeen = useRef<string | null>(null);
  if (expandTrigger && triggerSeen.current !== expandTrigger && !disabled) {
    triggerSeen.current = expandTrigger;
    setExpanded(true);
    requestAnimationFrame(() => inputRef.current?.focus());
  }

  const hasInFlightUpload = attachments.some((a) => a.status === 'uploading');
  const canSend =
    !disabled &&
    !isSending &&
    !submitting &&
    !hasInFlightUpload &&
    (text.trim().length > 0 || mentions.length > 0);

  const expand = useCallback(() => {
    if (disabled) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    setExpanded(true);
    onClearReplyTarget?.();
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [disabled, onClearReplyTarget]);

  const handleSubmit = useCallback(async () => {
    if (!canSend) return;
    const textSnap = text;
    const mentionsSnap = mentions;
    const attachmentsSnap = attachments;

    const mentionMd = serializeMentions(mentionsSnap);
    const trimmed = textSnap.trim();
    const content = mentionMd ? (trimmed ? `${mentionMd} ${trimmed}` : mentionMd) : trimmed;

    const activeIds = attachmentsSnap
      .filter((a) => a.status === 'completed')
      .map((a) => a.id)
      .filter((id): id is string => !!id);

    setText('');
    setAttachments([]);
    clearMentions();
    setSubmitting(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});

    try {
      await onSubmit({
        content,
        attachmentIds: activeIds,
        mentions: mentionsSnap,
      });
      inputRef.current?.blur();
      Keyboard.dismiss();
      setExpanded(false);
    } catch {
      setText(textSnap);
      setAttachments(attachmentsSnap);
      mentionsSnap.forEach((m) => useMentionDraftStore.getState().toggle(m));
    } finally {
      setSubmitting(false);
    }
  }, [canSend, text, mentions, attachments, setText, clearMentions, onSubmit]);

  const startUpload = useCallback(
    async (localId: string, asset: { uri: string; name: string; type: string }) => {
      try {
        const result = await api.uploadFile(asset, uploadContext);
        setAttachments((prev) =>
          prev.map((it) =>
            it.localId === localId
              ? {
                  ...it,
                  status: 'completed',
                  id: result.id,
                  url: result.url,
                  downloadUrl: result.download_url,
                }
              : it,
          ),
        );
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Unknown error';
        setAttachments((prev) =>
          prev.map((it) =>
            it.localId === localId ? { ...it, status: 'failed', error: message } : it,
          ),
        );
      }
    },
    [uploadContext],
  );

  const onImagePress = useCallback(async () => {
    const picker = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ImagePicker.MediaTypeOptions.Images,
      quality: 1,
    });
    if (picker.canceled) return;
    const picked = picker.assets[0];
    if (!picked) return;
    if (picked.fileSize != null && picked.fileSize > MAX_FILE_SIZE) {
      Alert.alert('File too large', 'Files must be smaller than 100 MB.');
      return;
    }
    const filename = picked.fileName ?? `image-${Date.now()}.jpg`;
    const mimeType = picked.mimeType ?? 'image/jpeg';
    const localId = makeLocalId();
    setAttachments((prev) => [
      ...prev,
      {
        localId,
        localUri: picked.uri,
        filename,
        mimeType,
        status: 'uploading',
      },
    ]);
    requestAnimationFrame(() => inputRef.current?.focus());
    await startUpload(localId, {
      uri: picked.uri,
      name: filename,
      type: mimeType,
    });
  }, [startUpload]);

  const onFilePress = useCallback(async () => {
    const picker = await DocumentPicker.getDocumentAsync({
      type: '*/*',
      copyToCacheDirectory: true,
    });
    if (picker.canceled) return;
    const picked = picker.assets[0];
    if (!picked) return;
    if (picked.size != null && picked.size > MAX_FILE_SIZE) {
      Alert.alert('File too large', 'Files must be smaller than 100 MB.');
      return;
    }
    const mimeType = picked.mimeType ?? 'application/octet-stream';
    const localId = makeLocalId();
    setAttachments((prev) => [
      ...prev,
      {
        localId,
        localUri: picked.uri,
        filename: picked.name,
        mimeType,
        status: 'uploading',
      },
    ]);
    requestAnimationFrame(() => inputRef.current?.focus());
    await startUpload(localId, {
      uri: picked.uri,
      name: picked.name,
      type: mimeType,
    });
  }, [startUpload]);

  const onRemoveAttachment = useCallback((localId: string) => {
    setAttachments((prev) => prev.filter((it) => it.localId !== localId));
  }, []);

  const onRetryAttachment = useCallback(
    (localId: string) => {
      const item = attachments.find((it) => it.localId === localId);
      if (!item) return;
      setAttachments((prev) =>
        prev.map((it) =>
          it.localId === localId ? { ...it, status: 'uploading', error: undefined } : it,
        ),
      );
      void startUpload(localId, {
        uri: item.localUri,
        name: item.filename,
        type: item.mimeType,
      });
    },
    [attachments, startUpload],
  );

  const onAtPress = useCallback(() => {
    Haptics.selectionAsync().catch(() => {});
    router.push(mentionPickerPath);
  }, [mentionPickerPath]);

  const onBlur = useCallback(() => {
    setTimeout(() => {
      const empty = text.trim().length === 0 && attachments.length === 0 && mentions.length === 0;
      if (empty && !inputRef.current?.isFocused()) {
        setExpanded(false);
        onClearReplyTarget?.();
      }
    }, 80);
  }, [text, attachments.length, mentions.length, onClearReplyTarget]);

  const pillContent = (
    <View
      className="border-t border-border bg-background px-3 pt-2"
      style={{ paddingBottom: (manageKeyboard ? insets.bottom : 0) + 8 }}
    >
      <Pressable
        onPress={expand}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel={pillLabel}
        accessibilityState={{ disabled }}
        className="flex-row items-center gap-2 h-11 px-4 rounded-full bg-secondary active:opacity-80"
      >
        <Ionicons name={pillIcon} size={18} color={theme.mutedForeground} />
        <Text className="text-base text-muted-foreground">
          {disabled && disabledReason ? disabledReason : pillLabel}
        </Text>
      </Pressable>
    </View>
  );

  const expandedContent = (
    <View
      className="bg-background px-3 pt-2 gap-2"
      style={{ paddingBottom: (manageKeyboard ? insets.bottom : 0) + 4 }}
    >
      {replyTarget && (
        <View className="px-3 py-1.5 rounded-md bg-secondary/60 gap-0.5">
          <View className="flex-row items-center gap-2">
            <Ionicons name="return-up-back" size={14} color={theme.mutedForeground} />
            <Text className="flex-1 text-xs font-medium text-muted-foreground" numberOfLines={1}>
              Replying to {replyTarget.actorName}
            </Text>
            <Pressable
              onPress={onClearReplyTarget}
              hitSlop={8}
              accessibilityRole="button"
              accessibilityLabel="Cancel reply"
            >
              <Ionicons name="close-circle" size={16} color={theme.mutedForeground} />
            </Pressable>
          </View>
          {replyTarget.preview ? (
            <Text className="text-xs text-muted-foreground pl-5" numberOfLines={2}>
              {stripMarkdown(replyTarget.preview)}
            </Text>
          ) : null}
        </View>
      )}

      <View
        className="rounded-3xl border border-border bg-secondary"
        style={{ borderCurve: 'continuous' }}
      >
        {mentions.length > 0 || attachments.length > 0 ? (
          <View className="px-2 pt-2 pb-1">
            <ComposerAttachmentRow
              mentions={mentions}
              attachments={attachments}
              onRemoveMention={removeMention}
              onRemoveAttachment={onRemoveAttachment}
              onRetryAttachment={onRetryAttachment}
            />
          </View>
        ) : null}

        <TextInput
          ref={inputRef}
          value={text}
          onChangeText={setText}
          onBlur={onBlur}
          placeholder={placeholder}
          placeholderTextColor={theme.mutedForeground}
          multiline
          editable={!disabled}
          className="px-4 pt-3 pb-1 text-base text-foreground"
          style={{ minHeight: 28, maxHeight: 140, textAlignVertical: 'top' }}
        />

        <View className="flex-row items-center px-2 pb-2 pt-1">
          {/* @ leads the toolbar — highest-signal attachment (only one
           *  that drives notifications) and cross-resource (people +
           *  issues), pride-of-place left. */}
          <IconButton
            name="at"
            iconSize={20}
            color={mentions.length > 0 ? theme.primary : undefined}
            onPress={onAtPress}
            accessibilityLabel="Mention someone or an issue"
            className="h-8 w-8"
          />
          <IconButton
            name="image-outline"
            iconSize={20}
            onPress={onImagePress}
            accessibilityLabel="Upload image"
            className="h-8 w-8"
          />
          <IconButton
            name="attach-outline"
            iconSize={20}
            onPress={onFilePress}
            accessibilityLabel="Upload file"
            className="h-8 w-8"
          />
          <View className="flex-1" />
          {isSending && renderStop ? (
            renderStop()
          ) : (
            <IconButton
              name="arrow-up"
              iconSize={18}
              color={theme.primaryForeground}
              variant="default"
              onPress={handleSubmit}
              disabled={!canSend}
              hitSlop={12}
              className="h-8 w-8 rounded-full"
              accessibilityLabel="Send"
              accessibilityState={{ disabled: !canSend }}
            />
          )}
        </View>
      </View>
    </View>
  );

  const body = expanded ? expandedContent : pillContent;

  if (!manageKeyboard) return body;

  return (
    <KeyboardStickyView offset={{ closed: 0, opened: insets.bottom }}>{body}</KeyboardStickyView>
  );
}
