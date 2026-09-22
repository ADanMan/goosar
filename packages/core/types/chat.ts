import type { AgentTask } from './agent';

export interface ChatPinnedAgent {
  agent_id: string;
  position: number;
}

export type ChatMessageKind = 'message' | 'no_response';

export interface ChatLastMessage {
  content: string;
  role: 'user' | 'assistant';
  created_at: string;
  failure_reason?: string | null;
  message_kind?: ChatMessageKind;
}

export interface ChatSession {
  id: string;
  workspace_id: string;
  agent_id: string;
  creator_id: string;
  project_id?: string | null;
  title: string;
  status: 'active' | 'archived';
  has_unread: boolean;
  unread_count?: number;
  last_message?: ChatLastMessage | null;
  pinned?: boolean;
  created_at: string;
  updated_at: string;
}

export interface PendingChatTaskItem {
  task_id: string;
  status: string;
  chat_session_id: string;
}

export interface PendingChatTasksResponse {
  tasks: PendingChatTaskItem[];
}

export interface HasPendingChatTasksResponse {
  has_pending: boolean;
}

export interface ChatMessage {
  id: string;
  chat_session_id: string;
  role: 'user' | 'assistant';
  content: string;
  task_id: string | null;
  created_at: string;
  attachments?: import('./attachment').Attachment[];
  failure_reason?: string | null;
  elapsed_ms?: number | null;
  message_kind?: ChatMessageKind;
}

export interface ChatMessagesCursor {
  created_at: string;
  id: string;
}

export interface ChatMessagesPage {
  messages: ChatMessage[];
  limit: number;
  has_more: boolean;
  next_cursor?: ChatMessagesCursor | null;
}

export interface SendChatMessageResponse {
  message_id: string;
  task_id: string;
  created_at: string;
  attachment_ids?: string[];
}

export interface CancelledChatMessage {
  chat_session_id: string;
  message_id: string;
  content: string;
  restore_to_input: boolean;
  attachments?: import('./attachment').Attachment[];
}

export interface CancelTaskResponse extends AgentTask {
  cancelled_chat_message?: CancelledChatMessage;
}

export interface ChatDraftRestore {
  id: string;
  chat_session_id: string;
  task_id?: string;
  content: string;
  attachments?: import('./attachment').Attachment[];
  created_at?: string;
}

export interface ChatDraftRestoresResponse {
  restores: ChatDraftRestore[];
}

export interface ChatPendingTask {
  task_id?: string;
  status?: string;
  created_at?: string;
}
