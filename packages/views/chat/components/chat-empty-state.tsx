'use client';

import type { Agent } from '@goosar/core/types';
import { ActorAvatar } from '../../common/actor-avatar';
import { useT } from '../../i18n';
import { ChatStarterCards, useChatStarterCards } from './chat-starter-cards';

export function EmptyState({
  agent,
  onPickStarter,
}: {
  agent: Agent | null;
  onPickStarter?: (message: string) => void | Promise<unknown>;
}) {
  const { t } = useT('chat');
  const starterCards = useChatStarterCards(agent);
  const description = agent?.description?.trim();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-8">
      {agent && (
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size="2xl"
          className="ring-1 ring-inset ring-border"
        />
      )}
      <div className="max-w-sm space-y-1 text-center">
        <h3 className="text-base font-semibold">
          {agent
            ? t(($) => $.empty_state.chat_with_named, { name: agent.name })
            : t(($) => $.empty_state.first_time_title)}
        </h3>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {onPickStarter && <ChatStarterCards cards={starterCards} onPick={onPickStarter} />}
    </div>
  );
}
