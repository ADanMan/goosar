import { ActivityIndicator, View } from 'react-native';
import { Redirect } from 'expo-router';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';

export default function Index() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (!user) return <Redirect href="/login" />;
  if (!slug) return <Redirect href="/select-workspace" />;
  return <Redirect href={`/${slug}/inbox`} />;
}
