-- 140_chat_read_state: per-session "last read" marker for chat sessions
-- (T-027 chat domain). A chat session has exactly one human reader — its
-- creator (schemas.ChatSession.creator_id) — so one timestamp column on
-- convos is enough; no separate per-account read-state table is needed
-- (contrast with alerts, which have many possible recipients).
--
-- Backs ChatSession.has_unread/unread_count (list only) and
-- POST /api/chat/sessions/{sessionId}/read (markChatSessionRead).

ALTER TABLE convos ADD COLUMN cv_last_read_at timestamptz NOT NULL DEFAULT now();
