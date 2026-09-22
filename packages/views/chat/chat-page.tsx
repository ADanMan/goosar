'use client';

import { useEffect, useRef, useState } from 'react';
import { useDefaultLayout } from 'react-resizable-panels';
import { ArrowLeft, MessageSquare } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@goosar/ui/components/ui/button';
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from '@goosar/ui/components/ui/resizable';
import { useIsMobile } from '@goosar/ui/hooks/use-mobile';
import { useWorkspacePaths } from '@goosar/core/paths';
import { useChatStore } from '@goosar/core/chat';
import type { Agent, ChatSession } from '@goosar/core/types';
import { PageHeader } from '../layout/page-header';
import { useNavigation } from '../navigation';
import { useT } from '../i18n';
import { ChatMessageList, ChatMessageSkeleton } from './components/chat-message-list';
import { ChatInput } from './components/chat-input';
import { ChatThreadList } from './components/chat-thread-list';
import { ChatSessionHeader } from './components/chat-session-header';
import { EmptyState } from './components/chat-empty-state';
import { NewChatButton } from './components/new-chat-button';
import { useChatController } from './components/use-chat-controller';
import { OfflineBanner } from './components/offline-banner';
import { NoAgentBanner } from './components/no-agent-banner';
import { ArchivedAgentBanner } from './components/archived-agent-banner';

export function ChatPage() {
  const { t } = useT('chat');
  const { searchParams, replace } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const isMobile = useIsMobile();

  const c = useChatController({ isActive: true });
  const urlSession = searchParams.get('session') || null;
  const urlAgent = searchParams.get('agent') || null;

  const [composingNew, setComposingNew] = useState(false);
  useEffect(() => {
    if (useChatStore.getState().activeSessionId) setComposingNew(false);
  }, [c.activeSessionId]);

  useEffect(() => {
    if (urlSession !== useChatStore.getState().activeSessionId) {
      c.setActiveSession(urlSession);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to URL only
  }, [urlSession]);

  useEffect(() => {
    const live = useChatStore.getState().activeSessionId;
    const current = searchParams.get('session') || null;
    if (live !== current) {
      const base = wsPaths.chat();
      replace(live ? `${base}?session=${live}` : base);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to store only
  }, [c.activeSessionId]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: 'goosar_chat_layout',
  });

  const consumedAgentIntent = useRef<string | null>(null);
  const supersedeAgentIntent = () => {
    if (urlAgent) consumedAgentIntent.current = urlAgent;
  };

  const handleSelect = (session: ChatSession) => {
    supersedeAgentIntent();
    c.handleSelectSession(session);
    setComposingNew(false);
  };

  const handleArchive = (session: ChatSession) => {
    supersedeAgentIntent();
    if (session.id === c.activeSessionId) {
      if (isMobile) {
        c.setActiveSession(null);
        setComposingNew(false);
      } else {
        c.advanceSelectionAfterArchive(session);
      }
    }
    c.archiveSession(session.id);
  };

  const startNewChat = (agent: Agent | null) => {
    supersedeAgentIntent();
    if (agent) c.handleStartNewChat(agent);
    else c.handleNewChat();
    setComposingNew(true);
  };

  const changeProjectContext = (projectId: string | null) => {
    if (projectId === c.activeProjectId) return;
    c.handleProjectChange(projectId);
    if (!c.currentSession || projectId !== null) setComposingNew(true);
  };

  useEffect(() => {
    if (!urlAgent) {
      consumedAgentIntent.current = null;
      return;
    }
    if (consumedAgentIntent.current === urlAgent) return;
    const agent = c.availableAgents.find((a) => a.id === urlAgent);
    if (agent) {
      consumedAgentIntent.current = urlAgent;
      startNewChat(agent);
      replace(wsPaths.chat());
      return;
    }
    if (c.agentsSettled) {
      consumedAgentIntent.current = urlAgent;
      toast.error(t(($) => $.page.agent_link_no_access));
      replace(wsPaths.chat());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume when the URL param or the resolving agent list changes
  }, [urlAgent, c.availableAgents, c.agentsSettled]);

  const newChatButton = (
    <NewChatButton
      agents={c.availableAgents}
      userId={c.user?.id}
      onStart={startNewChat}
      side="bottom"
    />
  );

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex items-center gap-2">
        <h1 className="text-sm font-semibold">{t(($) => $.page.title)}</h1>
      </div>
      {newChatButton}
    </PageHeader>
  );

  const listBody = (
    <div className="px-2 py-1">
      <ChatThreadList
        sessions={c.sessions}
        agents={c.agents}
        activeSessionId={c.activeSessionId}
        onSelectSession={handleSelect}
        onArchive={handleArchive}
      />
    </div>
  );

  const conversation = (
    <div className="flex flex-1 flex-col min-h-0">
      {c.currentSession && (
        <ChatSessionHeader
          session={c.currentSession}
          agent={c.activeAgent}
          onArchive={handleArchive}
        />
      )}
      {c.showSkeleton ? (
        <ChatMessageSkeleton />
      ) : c.hasMessages ? (
        <ChatMessageList
          key={c.activeSessionId}
          messages={c.messages}
          pendingTask={c.pendingTask}
          availability={c.availability}
          firstItemIndex={c.firstItemIndex}
          hasOlderMessages={c.hasOlderMessages}
          isFetchingOlderMessages={c.isFetchingOlderMessages}
          onLoadOlderMessages={() => void c.fetchOlderMessages()}
        />
      ) : (
        <EmptyState
          agent={c.activeAgent}
          onPickStarter={(message) => c.handleSend(message)}
        />
      )}

      {c.noAgent ? (
        <NoAgentBanner />
      ) : c.isAgentArchived ? (
        <ArchivedAgentBanner agentName={c.activeAgent?.name} />
      ) : (
        <OfflineBanner agentName={c.activeAgent?.name} availability={c.availability} />
      )}

      <ChatInput
        onSend={c.handleSend}
        restoreDraftRequest={c.restoreDraftRequest}
        onRestoreDraftApplied={c.handleRestoreDraftApplied}
        uploadEnabled={c.uploadEnabled}
        onStop={c.handleStop}
        isRunning={!!c.pendingTaskId}
        disabled={c.isSessionArchived || c.isAgentArchived}
        noAgent={c.noAgent}
        agentArchived={c.isAgentArchived}
        agentName={c.activeAgent?.name}
        projects={c.projects}
        projectId={c.activeProjectId}
        projectContextUnsupported={c.projectContextUnsupported}
        onProjectChange={changeProjectContext}
        isProjectUpdating={c.isProjectUpdating}
        focusRequest={c.focusInputRequest}
      />
    </div>
  );

  if (isMobile) {
    if (c.activeSessionId || composingNew) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                c.setActiveSession(null);
                setComposingNew(false);
              }}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.title)}
            </Button>
          </div>
          {conversation}
        </div>
      );
    }
    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <div className="flex-1 min-h-0 overflow-y-auto">{listBody}</div>
      </div>
    );
  }

  const hasTarget = !!c.activeSessionId || composingNew;
  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel
        id="list"
        defaultSize={320}
        minSize={240}
        maxSize={480}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="flex flex-col border-r h-full">
          {listHeader}
          <div className="flex-1 min-h-0 overflow-y-auto">{listBody}</div>
        </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
        <div className="flex flex-col min-h-0 h-full">
          {hasTarget ? (
            conversation
          ) : (
            <div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
              <MessageSquare className="h-10 w-10 text-muted-foreground/30" />
              <p className="text-sm">{t(($) => $.page.select_prompt)}</p>
            </div>
          )}
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
