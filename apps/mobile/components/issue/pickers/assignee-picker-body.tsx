/**
 * Тело пикера исполнителя — полиморфный одиночный выбор среди участников,
 * агентов и отрядов плюс опция «Не назначено». Заголовок и поиск — нативный
 * iOS-заголовок навигации, зарегистрированный в _layout.tsx; этот компонент —
 * просто FlatList без обвязки.
 */
import { useMemo } from 'react';
import { FlatList, Pressable, View } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import { Ionicons } from '@expo/vector-icons';
import { useColorScheme } from 'nativewind';
import type { Agent, IssueAssigneeType, MemberWithUser, Squad } from '@goosar/core/types';
import { Text } from '@/components/ui/text';
import { ActorAvatar } from '@/components/ui/actor-avatar';
import { memberListOptions } from '@/data/queries/members';
import { agentListOptions } from '@/data/queries/agents';
import { squadListOptions } from '@/data/queries/squads';
import { useWorkspaceStore } from '@/data/workspace-store';
import { useScrollToTopOnChange } from '@/lib/use-scroll-to-top-on-change';
import { THEME } from '@/lib/theme';

const AVATAR_SIZE = 36;

export type AssigneeValue = {
  type: IssueAssigneeType;
  id: string;
} | null;

interface Props {
  value: AssigneeValue;
  query: string;
  onChange: (next: AssigneeValue) => void;
}

type Row =
  | { kind: 'unassigned' }
  | { kind: 'member'; member: MemberWithUser }
  | { kind: 'agent'; agent: Agent }
  | { kind: 'squad'; squad: Squad };

function isRowSelected(value: AssigneeValue, row: Row): boolean {
  if (row.kind === 'unassigned') return value === null;
  if (value === null) return false;
  if (row.kind === 'member') return value.type === 'member' && value.id === row.member.user_id;
  if (row.kind === 'agent') return value.type === 'agent' && value.id === row.agent.id;
  return value.type === 'squad' && value.id === row.squad.id;
}

export function AssigneePickerBody({ value, query, onChange }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const listRef = useScrollToTopOnChange(query);
  const { colorScheme } = useColorScheme();
  const checkColor = colorScheme === 'dark' ? THEME.dark.primary : THEME.light.primary;

  const rows = useMemo<Row[]>(() => {
    const q = query.trim().toLowerCase();
    const matchName = (name: string) => !q || name.toLowerCase().includes(q);

    const memberRows: Row[] = [...members]
      .filter((m) => matchName(m.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((m) => ({ kind: 'member' as const, member: m }));
    const agentRows: Row[] = [...agents]
      .filter((a) => matchName(a.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((a) => ({ kind: 'agent' as const, agent: a }));
    const squadRows: Row[] = [...squads]
      .filter((s) => !s.archived_at && matchName(s.name))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((s) => ({ kind: 'squad' as const, squad: s }));

    if (q) return [...memberRows, ...agentRows, ...squadRows];

    const all = [...memberRows, ...agentRows, ...squadRows];
    const selectedRow = all.find((r) => isRowSelected(value, r));
    return [
      { kind: 'unassigned' },
      ...(selectedRow ? [selectedRow] : []),
      ...memberRows.filter((r) => !isRowSelected(value, r)),
      ...agentRows.filter((r) => !isRowSelected(value, r)),
      ...squadRows.filter((r) => !isRowSelected(value, r)),
    ];
  }, [members, agents, squads, query, value]);

  const isSelected = (row: Row) => isRowSelected(value, row);

  const select = (row: Row) => {
    if (row.kind === 'unassigned') onChange(null);
    else if (row.kind === 'member') onChange({ type: 'member', id: row.member.user_id });
    else if (row.kind === 'agent') onChange({ type: 'agent', id: row.agent.id });
    else onChange({ type: 'squad', id: row.squad.id });
  };

  return (
    <FlatList
      ref={listRef}
      data={rows}
      className="flex-1"
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      contentInsetAdjustmentBehavior="automatic"
      keyExtractor={(row) => {
        if (row.kind === 'unassigned') return 'unassigned';
        if (row.kind === 'member') return `m:${row.member.user_id}`;
        if (row.kind === 'agent') return `a:${row.agent.id}`;
        return `s:${row.squad.id}`;
      }}
      renderItem={({ item }) => (
        <Pressable
          onPress={() => select(item)}
          className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
        >
          {item.kind === 'unassigned' ? (
            <View
              className="rounded-full border border-dashed border-muted-foreground/40 items-center justify-center"
              style={{ width: AVATAR_SIZE, height: AVATAR_SIZE }}
            >
              <Text className="text-sm text-muted-foreground">∅</Text>
            </View>
          ) : item.kind === 'member' ? (
            <ActorAvatar type="member" id={item.member.user_id} size={AVATAR_SIZE} />
          ) : item.kind === 'agent' ? (
            <ActorAvatar type="agent" id={item.agent.id} size={AVATAR_SIZE} />
          ) : (
            <ActorAvatar type="squad" id={item.squad.id} size={AVATAR_SIZE} />
          )}
          <Text className="flex-1 text-base text-foreground">
            {item.kind === 'unassigned'
              ? 'Unassigned'
              : item.kind === 'member'
                ? item.member.name
                : item.kind === 'agent'
                  ? item.agent.name
                  : item.squad.name}
          </Text>
          {/* Right-aligned secondary label. Mirrors Apple's
              UITableViewCellStyleValue1 / UIListContentConfiguration.valueCell
              pattern used throughout iOS Settings — type tag in lighter font on
              the same row. Members carry no tag (they're the default actor). */}
          {item.kind === 'agent' ? (
            <Text className="text-sm text-muted-foreground">Agent</Text>
          ) : item.kind === 'squad' ? (
            <Text className="text-sm text-muted-foreground">Squad</Text>
          ) : null}
          {isSelected(item) ? <Ionicons name="checkmark" size={20} color={checkColor} /> : null}
        </Pressable>
      )}
      ListEmptyComponent={
        <View className="px-3 py-8 items-center">
          <Text className="text-sm text-muted-foreground">No matches.</Text>
        </View>
      }
    />
  );
}
