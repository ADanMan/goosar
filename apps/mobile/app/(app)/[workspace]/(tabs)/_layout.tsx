// Нижняя панель вкладок — JS <Tabs> из expo-router. NativeTabs не подходит:
// нельзя перехватить нажатие вкладки, а «Ещё» должно открывать меню, а не экран.
import { useRef } from 'react';
import { Tabs } from 'expo-router';
import { Image } from 'expo-image';
import { View } from 'react-native';
import type { TriggerRef } from '@rn-primitives/dropdown-menu';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useColorScheme } from '@/lib/use-color-scheme';
import { THEME } from '@/lib/theme';
import { useInboxUnreadCount, useChatUnreadMessageCount } from '@/lib/unread-counts';
import { MoreTabDropdownAnchor } from '@/components/nav/more-tab-dropdown';

const BADGE_STYLE = {
  backgroundColor: THEME.light.brand,
};

export default function TabsLayout() {
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];

  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const inboxUnread = useInboxUnreadCount(wsId);
  const chatUnread = useChatUnreadMessageCount(wsId);

  const inboxBadge = inboxUnread > 0 ? (inboxUnread > 99 ? '99+' : String(inboxUnread)) : undefined;
  const chatBadge = chatUnread > 0 ? (chatUnread > 99 ? '99+' : String(chatUnread)) : undefined;

  const moreTriggerRef = useRef<TriggerRef>(null);

  return (
    <View style={{ flex: 1 }}>
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarActiveTintColor: t.foreground,
          tabBarInactiveTintColor: t.mutedForeground,
          tabBarStyle: { backgroundColor: t.background },
          tabBarLabelStyle: { fontSize: 11 },
        }}
      >
        <Tabs.Screen
          name="inbox"
          options={{
            title: 'Inbox',
            tabBarBadge: inboxBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? 'sf:tray.fill' : 'sf:tray'}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="my-issues"
          options={{
            title: 'My Issues',
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? 'sf:checklist' : 'sf:checklist.unchecked'}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="chat"
          options={{
            title: 'Chat',
            tabBarBadge: chatBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? 'sf:bubble.left.fill' : 'sf:bubble.left'}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="more"
          options={{
            title: 'More',
            tabBarIcon: ({ color, size }) => (
              <Image source="sf:ellipsis" tintColor={color} style={{ width: size, height: size }} />
            ),
          }}
          listeners={() => ({
            tabPress: (e) => {
              e.preventDefault();
              moreTriggerRef.current?.open();
            },
          })}
        />
      </Tabs>

      <MoreTabDropdownAnchor triggerRef={moreTriggerRef} />
    </View>
  );
}
