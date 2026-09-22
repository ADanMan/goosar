export type CommentType = 'comment' | 'status_change' | 'progress_update' | 'system';

export type CommentAuthorType = 'member' | 'agent' | 'system';

export interface Reaction {
  id: string;
  comment_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export interface Comment {
  id: string;
  issue_id: string;
  author_type: CommentAuthorType;
  author_id: string;
  content: string;
  type: CommentType;
  parent_id: string | null;
  reactions: Reaction[];
  attachments: import('./attachment').Attachment[];
  created_at: string;
  updated_at: string;
  resolved_at: string | null;
  resolved_by_type: CommentAuthorType | null;
  resolved_by_id: string | null;
  source_task_id?: string | null;
  trigger_outcomes?: CommentTriggerOutcome[];
}

export type CommentTriggerStatus = 'queued' | 'coalesced' | 'deferred' | 'blocked';

export interface CommentTriggerOutcome {
  target_type: string; 
  target_id: string;
  status: CommentTriggerStatus | string;
  reason_code: string;
}

export type CommentTriggerSource = 'issue_assignee' | 'mention_agent' | 'mention_squad_leader';

export interface CommentTriggerPreviewAgent {
  id: string;
  name: string;
  avatar_url?: string;
  source: CommentTriggerSource | string;
  reason: string;
}

export interface CommentTriggerPreview {
  agents: CommentTriggerPreviewAgent[];
  blocked?: CommentTriggerOutcome[];
}
