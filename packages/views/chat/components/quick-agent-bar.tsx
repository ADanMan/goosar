'use client';

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useWorkspaceId } from '@goosar/core/hooks';
import { chatPinnedAgentsOptions } from '@goosar/core/chat/queries';
import { usePinChatAgent, useUnpinChatAgent } from '@goosar/core/chat/mutations';
import type { Agent } from '@goosar/core/types';
import {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
} from '@goosar/ui/components/ui/context-menu';
import { ActorAvatar } from '../../common/actor-avatar';
import { AgentPicker } from './new-chat-button';
import { useT } from '../../i18n';

const AVATAR_RING = 'ring-1 ring-inset ring-border';

const MAX_PINNED = 5;

export function QuickAgentBar({
  agents,
  userId,
  onStartNewChat,
}: {
  agents: Agent[];
  userId: string | undefined;
  onStartNewChat: (agent: Agent) => void;
}) {
  const { t } = useT('chat');
  const wsId = useWorkspaceId();
  const { data: pinned = [] } = useQuery(chatPinnedAgentsOptions(wsId));
  const pin = usePinChatAgent();
  const unpin = useUnpinChatAgent();

  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const pinnedAgents = useMemo(
    () => pinned.map((p) => agentById.get(p.agent_id)).filter((a): a is Agent => !!a),
    [pinned, agentById],
  );
  const pinnedIds = useMemo(() => new Set(pinned.map((p) => p.agent_id)), [pinned]);
  const addable = useMemo(() => agents.filter((a) => !pinnedIds.has(a.id)), [agents, pinnedIds]);
  const canAdd = addable.length > 0 && pinnedAgents.length < MAX_PINNED;

  if (agents.length === 0) return null;
  if (pinnedAgents.length === 0 && !canAdd) return null;

  return (
    <div className="flex items-center gap-1.5 overflow-x-auto border-b px-2 py-1.5">
      {pinnedAgents.map((agent) => (
        <ContextMenu key={agent.id}>
          <ContextMenuTrigger
            render={
              <button
                type="button"
                aria-label={agent.name}
                onClick={() => onStartNewChat(agent)}
                className="flex size-[34px] shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            }
          >
            <ActorAvatar
              actorType="agent"
              actorId={agent.id}
              size="lg"
              showStatusDot
              enableHoverCard
              profileLink={false}
              className={AVATAR_RING}
            />
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem variant="destructive" onClick={() => unpin.mutate(agent.id)}>
              {t(($) => $.list.unpin_agent)}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ))}

      {canAdd && (
        <AgentPicker
          agents={addable}
          userId={userId}
          onSelect={(agent) => pin.mutate(agent.id)}
          side="bottom"
          align="start"
          triggerRender={
            <button
              type="button"
              aria-label={t(($) => $.list.add_agent)}
              className="flex size-[34px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground ring-1 ring-inset ring-border outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
            />
          }
          trigger={<Plus className="size-4" />}
        />
      )}
    </div>
  );
}
