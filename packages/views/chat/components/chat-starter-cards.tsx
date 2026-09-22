'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWorkspaceId } from '@goosar/core/hooks';
import { workspaceCapabilitiesOptions } from '@goosar/core/workspace/queries';
import type { Agent, WorkspaceSampleTask } from '@goosar/core/types';
import { useT } from '../../i18n';
import { HELPER_STARTER_PROMPTS, pickContentLang } from '../../onboarding/templates';
import type { ContentLang } from '../../onboarding/templates';
import { isWorkspaceHelper } from '../../workspace/helper-setup';

export interface ChatStarterCard {
  key: string;
  label: string;
  message: string;
}

const MAX_STARTER_CARDS = 3;

const FALLBACK_CARD_IDS = ['intro', 'tour'] as const;

export function resolveStarterCards(input: {
  sampleTasks: readonly WorkspaceSampleTask[] | undefined;
  lang: ContentLang;
}): ChatStarterCard[] {
  const fromRole = (input.sampleTasks ?? [])
    .map((task, index) => ({
      key: task.key || `task-${index}`,
      label: task.title.trim(),
      message: task.title.trim(),
    }))
    .filter((card) => card.label !== '')
    .slice(0, MAX_STARTER_CARDS);
  if (fromRole.length > 0) return fromRole;

  return FALLBACK_CARD_IDS.map((id) => ({
    key: id,
    label: HELPER_STARTER_PROMPTS[id].title[input.lang],
    message: HELPER_STARTER_PROMPTS[id].prompt[input.lang],
  }));
}

export function useChatStarterCards(agent: Agent | null): ChatStarterCard[] {
  const { i18n } = useT('chat');
  const wsId = useWorkspaceId();
  const isHelper = !!agent && isWorkspaceHelper(agent);
  const { data: role, isPending } = useQuery({
    ...workspaceCapabilitiesOptions(wsId),
    enabled: wsId !== '' && isHelper,
  });

  if (!isHelper || isPending) return [];
  return resolveStarterCards({
    sampleTasks: role?.sample_tasks,
    lang: pickContentLang(i18n.language),
  });
}

export function ChatStarterCards({
  cards,
  onPick,
}: {
  cards: readonly ChatStarterCard[];
  onPick: (message: string) => void | Promise<unknown>;
}) {
  const { t } = useT('chat');
  const [sending, setSending] = useState(false);
  if (cards.length === 0) return null;
  const pick = async (message: string) => {
    if (sending) return;
    setSending(true);
    try {
      await onPick(message);
    } finally {
      setSending(false);
    }
  };
  return (
    <div className="w-full max-w-sm space-y-2">
      <p className="text-center text-xs text-muted-foreground">
        {t(($) => $.starter_cards.heading)}
      </p>
      {cards.map((card) => (
        <button
          key={card.key}
          type="button"
          disabled={sending}
          onClick={() => void pick(card.message)}
          className="w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-sm text-foreground transition-colors hover:border-brand/40 hover:bg-accent disabled:pointer-events-none disabled:opacity-60"
        >
          {card.label}
        </button>
      ))}
    </div>
  );
}
