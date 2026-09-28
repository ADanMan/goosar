package chat

import (
	"context"
	"fmt"

	"github.com/adanman/goosar/server2/internal/realtime"
)

// FinalizeRunParams — вход FinalizeAgentReply.
type FinalizeRunParams struct {
	WorkspaceID string
	ConvoID     string
	TaskID      string // dispatch_jobs.id завершённого запуска

	// Content — текст ответа агента. Пусто, если Kind == "no_response"
	// (агент осознанно не дал текстового ответа — contract ChatMessage.message_kind).
	Content string
	Kind    string // message|no_response, по умолчанию "message"

	FailureReason *string // заполняется, если запуск завершился ошибкой (task:failed)
	ElapsedMs     *int
}

// AppendAgentReply — внутренний API пакета chat для протокола daemon (T-028):
// вызывается, когда завершённый (успешно или нет) запуск dispatch_jobs с
// dj_kind='chat' готов получить ответное сообщение чата. Создаёт сообщение
// role=assistant, публикует chat:message и завершающий chat:done — ровно
// последовательность, которую описывает sendChatMessage в контракте
// ("Одновременно с завершением сервис создаёт ассистентское сообщение чата
// и публикует chat:message (role=assistant) и затем chat:done").
//
// T-028 вызывает это из своего обработчика финализации запуска (daemon
// сообщает done/failed), передав dj_context уже известные workspace_id/
// convo_id/task_id из самой dispatch_jobs строки — chat не переоткрывает её.
func (d *Deps) AppendAgentReply(ctx context.Context, p FinalizeRunParams) (Message, error) {
	kind := p.Kind
	if kind == "" {
		kind = "message"
	}
	msg, err := d.Store.CreateMessage(ctx, CreateMessageParams{
		ConvoID: p.ConvoID, Role: "assistant", Content: p.Content, TaskID: strPtrOrNil(p.TaskID), Kind: kind,
	})
	if err != nil {
		return Message{}, fmt.Errorf("chat: сохранение ответа агента: %w", err)
	}
	if p.FailureReason != nil || p.ElapsedMs != nil {
		if err := d.Store.setMessageOutcome(ctx, msg.ID, p.FailureReason, p.ElapsedMs); err != nil {
			return Message{}, err
		}
	}
	if d.Publisher != nil {
		d.Publisher.Publish(p.WorkspaceID, realtime.Event{Type: "chat:message", Payload: map[string]any{
			"chat_session_id": p.ConvoID, "message_id": msg.ID, "role": "assistant", "content": p.Content,
			"task_id": p.TaskID, "created_at": msg.CreatedAt,
		}})
		d.Publisher.Publish(p.WorkspaceID, realtime.Event{Type: "chat:done", Payload: map[string]any{
			"chat_session_id": p.ConvoID, "task_id": p.TaskID, "message_id": msg.ID, "content": p.Content,
			"elapsed_ms": p.ElapsedMs, "message_kind": kind,
		}})
	}
	return msg, nil
}

// AppendSystemNote — вспомогательная точка входа для T-028: записывает
// служебное сообщение role=system (например "агент недоступен") без
// публикации chat:done — используется, когда протокол daemon сам решает,
// нужен ли завершающий сигнал.
func (d *Deps) AppendSystemNote(ctx context.Context, workspaceID, convoID, content string) (Message, error) {
	msg, err := d.Store.CreateMessage(ctx, CreateMessageParams{ConvoID: convoID, Role: "system", Content: content})
	if err != nil {
		return Message{}, fmt.Errorf("chat: системное сообщение: %w", err)
	}
	if d.Publisher != nil {
		d.Publisher.Publish(workspaceID, realtime.Event{Type: "chat:message", Payload: map[string]any{
			"chat_session_id": convoID, "message_id": msg.ID, "role": "system", "content": content, "created_at": msg.CreatedAt,
		}})
	}
	return msg, nil
}
