// Заглушка маршрута: вкладка «Ещё» перехватывает нажатие и открывает меню
// форм-листом, поэтому этот экран в обычном сценарии не рендерится. Файл нужен,
// чтобы expo-router зарегистрировал вкладку.
import { Redirect } from 'expo-router';
import { useWorkspaceStore } from '@/data/workspace-store';

export default function MoreStub() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  return <Redirect href={slug ? `/${slug}/inbox` : '/select-workspace'} />;
}
