// Выбор упоминания уровня воркспейса (formSheet), открывается из любого композера
// с кнопкой @. Параметр mode задаёт набор секций.
import { useLocalSearchParams } from 'expo-router';
import { MentionPickerBody } from '@/components/issue/pickers/mention-picker-body';
import { useNativeSearchBar } from '@/lib/use-native-search-bar';

type Mode = 'comment' | 'chat';

export default function MentionPickerRoute() {
  const { mode: rawMode } = useLocalSearchParams<{ mode?: string }>();
  const mode: Mode = rawMode === 'chat' ? 'chat' : 'comment';
  const placeholder = mode === 'chat' ? 'Reference an issue' : 'Search people or issues';
  const query = useNativeSearchBar(placeholder, { autoFocus: true });
  return <MentionPickerBody mode={mode} query={query} />;
}
