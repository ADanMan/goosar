// Композер чата — тонкая обёртка над общим MessageComposer с чат-специфичной
// обвязкой: контролируемый текст (черновик хранится в useChatDraftsStore по сессиям).
import { useCallback } from 'react';
import { Pressable, View } from 'react-native';
import Animated, { FadeIn, FadeOut } from 'react-native-reanimated';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { MessageComposer } from '@/components/composer/message-composer';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';

interface Props {
  value: string;
  onChangeText: (next: string) => void;
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  onStop: () => void;
  sending: boolean;
  disabled?: boolean;
  disabledReason?: string;
}

const IS_IOS = process.env.EXPO_OS === 'ios';

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  sending,
  disabled = false,
  disabledReason,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const onSubmit = useCallback(
    async ({ content, attachmentIds }: { content: string; attachmentIds: string[] }) => {
      await onSend(content, attachmentIds);
    },
    [onSend],
  );

  const handleStop = useCallback(() => {
    if (IS_IOS) {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    }
    onStop();
  }, [onStop]);

  return (
    <MessageComposer
      value={value}
      onChangeText={onChangeText}
      onSubmit={onSubmit}
      mentionPickerPath={{
        pathname: '/[workspace]/mention-picker',
        params: { workspace: wsSlug ?? '', mode: 'chat' },
      }}
      placeholder={sending ? 'Agent is working…' : 'Message…'}
      pillLabel={
        sending
          ? 'Agent is working…'
          : disabled
            ? (disabledReason ?? 'Chat unavailable')
            : 'Message…'
      }
      pillIcon="chatbubble-ellipses-outline"
      disabled={disabled}
      disabledReason={disabledReason}
      isSending={sending}
      renderStop={() => <StopButton onPress={handleStop} />}
      manageKeyboard={false}
    />
  );
}

function StopButton({ onPress }: { onPress: () => void }) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  return (
    <Animated.View key="stop" entering={FadeIn.duration(120)} exiting={FadeOut.duration(120)}>
      <Pressable
        onPress={onPress}
        className="h-8 w-8 items-center justify-center rounded-full bg-foreground active:opacity-80"
        hitSlop={12}
        accessibilityRole="button"
        accessibilityLabel="Stop agent"
      >
        <View
          style={{
            width: 10,
            height: 10,
            backgroundColor: theme.background,
            borderRadius: 1.5,
          }}
        />
      </Pressable>
    </Animated.View>
  );
}
