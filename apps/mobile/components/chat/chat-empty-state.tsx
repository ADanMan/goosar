// Пустое состояние активной сессии без сообщений. Два режима, как в вебе:
// первый чат в воркспейсе — обучающий, иначе — обычный.
import { View } from 'react-native';
import { Text } from '@/components/ui/text';
import { Button } from '@/components/ui/button';

const STARTER_PROMPTS: { icon: string; text: string }[] = [
  { icon: '📋', text: 'List my open issues by priority' },
  { icon: '📝', text: 'Summarize what I did today' },
  { icon: '💡', text: 'Help me plan what to do next' },
];

interface Props {
  hasSessions: boolean;
  agentName?: string;
  onPickPrompt: (text: string) => void;
}

export function ChatEmptyState({ hasSessions, agentName, onPickPrompt }: Props) {
  if (!hasSessions) {
    return (
      <View className="flex-1 items-center justify-center px-6 py-8">
        <View className="max-w-xs items-center gap-3">
          <Text className="text-base font-semibold text-foreground text-center">
            Chat with your agents
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            <Text className="text-sm text-muted-foreground">✨ They know your workspace — </Text>
            <Text className="text-sm font-medium text-foreground">issues, projects, skills</Text>
            <Text className="text-sm text-muted-foreground">.</Text>
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            Ask for a summary, plan your day, or hand off a small task.
          </Text>
        </View>
      </View>
    );
  }

  const title = agentName ? `Hi, I'm ${agentName}` : 'Welcome back to Goosar';
  return (
    <View className="flex-1 items-center justify-center px-6 py-8 gap-5">
      <View className="items-center gap-1">
        <Text className="text-base font-semibold text-foreground text-center">{title}</Text>
        <Text className="text-sm text-muted-foreground text-center">Try asking</Text>
      </View>
      <View className="w-full max-w-xs gap-2">
        {STARTER_PROMPTS.map((p) => (
          <Button
            key={p.text}
            variant="outline"
            onPress={() => onPickPrompt(p.text)}
            className="h-auto justify-start px-3 py-2.5"
            accessibilityLabel={p.text}
          >
            <Text className="text-sm text-foreground">
              <Text className="text-sm">{p.icon} </Text>
              {p.text}
            </Text>
          </Button>
        ))}
      </View>
    </View>
  );
}
