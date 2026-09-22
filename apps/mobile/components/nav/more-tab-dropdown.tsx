/**
 * Всплывающее меню, открывающееся по тапу на вкладку «More». Монтируется
 * как отдельный элемент рядом с Tabs, а не как сама кнопка таба, — так
 * кнопка остаётся обычным React Navigation Pressable и не теряет
 * визуальное соответствие остальным вкладкам.
 *
 * Обёртка позиционируется поверх области таба и пропускает тапы вниз
 * (pointerEvents box-none); меню открывается императивно из tabPress.
 * Цвета берутся из токенов THEME, поэтому тёмная тема работает сама.
 * Воркспейс свёрнут в одну карточку с переходом на экран смены
 * воркспейса с подтверждением.
 */

import { useMemo } from 'react';
import { Image, Pressable, View } from 'react-native';
import { Image as ExpoImage } from 'expo-image';
import { router, usePathname } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { TriggerRef } from '@rn-primitives/dropdown-menu';
import type { User, Workspace } from '@goosar/core/types';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu';
import { Text } from '@/components/ui/text';
import { WorkspaceAvatar } from '@/components/workspace/workspace-avatar';
import { workspaceListOptions } from '@/data/queries/workspaces';
import { useAuthStore } from '@/data/auth-store';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { cn } from '@/lib/utils';

const TAB_BAR_HEIGHT = 49;

interface NavItem {
  label: string;
  icon: string;
  path: string;
}

const NAV_ITEMS: NavItem[] = [
  { label: 'Pinned', icon: 'pin', path: '/more/pins' },
  { label: 'Issues', icon: 'list.bullet', path: '/more/issues' },
  { label: 'Projects', icon: 'square.stack', path: '/more/projects' },
];

export function MoreTabDropdownAnchor({
  triggerRef,
}: {
  triggerRef: React.RefObject<TriggerRef | null>;
}) {
  const insets = useSafeAreaInsets();
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const pathname = usePathname();
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];
  const currentWorkspace = useCurrentWorkspace(slug);

  const isActive = (path: string) => {
    if (!slug) return false;
    const target = `/${slug}${path}`;
    return pathname === target || pathname.startsWith(target + '/');
  };

  return (
    <View
      pointerEvents="box-none"
      style={{
        position: 'absolute',
        right: 0,
        bottom: insets.bottom,
        width: '25%',
        height: TAB_BAR_HEIGHT,
      }}
    >
      <DropdownMenu>
        <DropdownMenuTrigger ref={triggerRef} asChild>
          {/* Invisible, non-tappable: the real tab button below catches
              all touches; we open this trigger imperatively via ref.
              The Pressable just provides a measurable rect for the
              popover to anchor against. */}
          <Pressable
            pointerEvents="none"
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
            style={{ width: '100%', height: '100%' }}
          />
        </DropdownMenuTrigger>

        <DropdownMenuContent side="top" align="end" sideOffset={6} className="w-72 p-2">
          <UserCard
            user={user}
            onPress={() => slug && router.push(`/${slug}/more/settings`)}
            chevronTint={t.mutedForeground}
          />

          <DropdownMenuSeparator />

          <WorkspaceCard
            currentWorkspaceName={currentWorkspace?.name}
            currentWorkspaceAvatarUrl={currentWorkspace?.avatar_url}
            onPress={() => slug && router.push(`/${slug}/switch-workspace`)}
            chevronTint={t.mutedForeground}
          />

          <DropdownMenuSeparator />

          {NAV_ITEMS.map((item) => (
            <DropdownMenuItem
              key={item.path}
              onPress={() => slug && router.push(`/${slug}${item.path}`)}
              accessibilityLabel={item.label}
              className={cn('h-9 gap-3', isActive(item.path) && 'bg-secondary')}
            >
              <ExpoImage
                source={`sf:${item.icon}`}
                tintColor={t.foreground}
                style={{ width: 18, height: 18 }}
              />
              <Text className="text-sm text-foreground">{item.label}</Text>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </View>
  );
}

function UserCard({
  user,
  onPress,
  chevronTint,
}: {
  user: User | null;
  onPress: () => void;
  chevronTint: string;
}) {
  const initial = (user?.name ?? user?.email ?? 'U').charAt(0).toUpperCase();
  return (
    <DropdownMenuItem
      onPress={onPress}
      className="h-12 gap-3"
      accessibilityLabel="Account settings"
    >
      {user?.avatar_url ? (
        <Image source={{ uri: user.avatar_url }} className="size-8 rounded-full bg-muted" />
      ) : (
        <View className="size-8 rounded-full bg-muted items-center justify-center">
          <Text className="text-xs font-medium text-muted-foreground">{initial}</Text>
        </View>
      )}
      <View className="flex-1 min-w-0">
        <Text className="text-sm font-medium text-foreground" numberOfLines={1}>
          {user?.name ?? '—'}
        </Text>
        {user?.email ? (
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            {user.email}
          </Text>
        ) : null}
      </View>
      <ExpoImage
        source="sf:chevron.right"
        tintColor={chevronTint}
        style={{ width: 12, height: 12 }}
      />
    </DropdownMenuItem>
  );
}

function WorkspaceCard({
  currentWorkspaceName,
  currentWorkspaceAvatarUrl,
  onPress,
  chevronTint,
}: {
  currentWorkspaceName: string | undefined;
  currentWorkspaceAvatarUrl: string | null | undefined;
  onPress: () => void;
  chevronTint: string;
}) {
  const { data } = useQuery(workspaceListOptions());
  const canSwitch = (data?.length ?? 0) > 1;

  return (
    <DropdownMenuItem
      onPress={onPress}
      disabled={!canSwitch}
      className="h-12 gap-3"
      accessibilityLabel={canSwitch ? 'Switch workspace' : (currentWorkspaceName ?? 'Workspace')}
    >
      <WorkspaceAvatar
        name={currentWorkspaceName ?? 'Workspace'}
        avatarUrl={currentWorkspaceAvatarUrl}
        size={32}
      />
      <View className="flex-1 min-w-0">
        <Text className="text-sm font-medium text-foreground" numberOfLines={1}>
          {currentWorkspaceName ?? 'Workspace'}
        </Text>
      </View>
      {canSwitch ? (
        <ExpoImage
          source="sf:chevron.right"
          tintColor={chevronTint}
          style={{ width: 12, height: 12 }}
        />
      ) : null}
    </DropdownMenuItem>
  );
}

function useCurrentWorkspace(slug: string | null): Workspace | undefined {
  const { data } = useQuery(workspaceListOptions());
  return useMemo(() => (slug ? data?.find((w) => w.slug === slug) : undefined), [data, slug]);
}
