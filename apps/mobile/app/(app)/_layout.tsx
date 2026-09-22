import { Stack, Redirect } from 'expo-router';
import { useAuthStore } from '@/data/auth-store';

export default function AppLayout() {
  const user = useAuthStore((s) => s.user);
  if (!user) return <Redirect href="/login" />;
  return <Stack screenOptions={{ headerShown: false }} />;
}
