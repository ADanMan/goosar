package handler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const chatTitleGenTimeout = 20 * time.Second

const chatTitleSystemPrompt = `You write a very short title that summarizes the topic of a chat conversation, given the user's opening message.

Rules:
- Output ONLY the title text — nothing else, no explanation.
- Keep it short: a few words, ideally under 8, never a full sentence.
- Write the title in the SAME language as the user's message (Chinese input → Chinese title, English input → English title).
- Do NOT wrap the title in quotes or brackets.
- Do NOT prefix it with "Title:", "标题：", or similar.
- Do NOT end with a period or any trailing punctuation.`

func (h *Handler) maybeGenerateChatTitleAsync(workspaceID, userID string, sessionID pgtype.UUID, currentTitle, sourceText string) {

	if h.LLM == nil || !h.LLM.Enabled() {
		return
	}
	if strings.TrimSpace(sourceText) == "" {
		return
	}

	go func() {

		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("chat title generation panicked; keeping original title",
					"session_id", uuidToString(sessionID),
					"panic", rec,
				)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), chatTitleGenTimeout)
		defer cancel()

		updated, applied, err := h.generateChatSessionTitle(ctx, sessionID, currentTitle, sourceText)
		if err != nil {

			slog.Warn("chat title generation failed; keeping original title",
				"session_id", uuidToString(sessionID),
				"error", err,
			)
			return
		}
		if !applied {

			return
		}

		resolvedSessionID := uuidToString(updated.ID)
		h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
			ChatSessionID: resolvedSessionID,
			Title:         updated.Title,
			UpdatedAt:     timestampToString(updated.UpdatedAt),
		})
	}()
}

func (h *Handler) generateChatSessionTitle(ctx context.Context, sessionID pgtype.UUID, currentTitle, sourceText string) (db.ChatSession, bool, error) {

	raw, err := h.LLM.GenerateText(ctx, "", chatTitleSystemPrompt, sourceText)
	if err != nil {
		return db.ChatSession{}, false, err
	}

	title := sanitizeChatTitle(raw)
	if title == "" {

		return db.ChatSession{}, false, nil
	}

	updated, err := h.Queries.UpdateChatSessionTitleIfCurrent(ctx, db.UpdateChatSessionTitleIfCurrentParams{
		ID:            sessionID,
		ExpectedTitle: currentTitle,
		NewTitle:      title,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {

			return db.ChatSession{}, false, nil
		}
		return db.ChatSession{}, false, err
	}
	return updated, true, nil
}

var chatTitleLabelPrefixes = []string{
	"title:", "title：",
	"标题:", "标题：",
	"题目:", "题目：",
	"主题:", "主题：",
}

func sanitizeChatTitle(raw string) string {

	s := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	if s == "" {
		return ""
	}

	for {
		before := s
		s = strings.TrimSpace(stripChatTitleLabelPrefix(s))
		s = stripSurroundingQuotes(s)

		s = strings.TrimSpace(strings.TrimRight(s, ".。!！?？,，;；:：、 "))
		if s == before || s == "" {
			break
		}
	}
	if s == "" {
		return ""
	}

	if runes := []rune(s); len(runes) > chatSessionTitleMaxLen {
		s = strings.TrimSpace(string(runes[:chatSessionTitleMaxLen]))
	}
	return s
}

func stripChatTitleLabelPrefix(s string) string {
	lower := strings.ToLower(s)
	for _, p := range chatTitleLabelPrefixes {
		if strings.HasPrefix(lower, p) {
			return s[len(p):]
		}
	}
	return s
}

var chatTitleQuotePairs = map[rune]rune{
	'"':  '"',
	'\'': '\'',
	'`':  '`',
	'“':  '”',
	'‘':  '’',
	'「':  '」',
	'『':  '』',
	'《':  '》',
	'（':  '）',
	'(':  ')',
	'【':  '】',
	'[':  ']',
}

func stripSurroundingQuotes(s string) string {
	for {
		runes := []rune(s)
		if len(runes) < 2 {
			return s
		}
		closer, ok := chatTitleQuotePairs[runes[0]]
		if !ok || runes[len(runes)-1] != closer {
			return s
		}
		s = strings.TrimSpace(string(runes[1 : len(runes)-1]))
		if s == "" {
			return s
		}
	}
}
