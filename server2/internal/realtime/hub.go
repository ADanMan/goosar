// Package realtime — хаб WebSocket-подключений клиента (/ws). Полный
// протокол подписок и типов событий описан в сопроводительном A.md
// ("Протокол /ws"), который вне списка файлов, разрешённых в этой
// clean-room сессии (см. server2/docs/decisions.md, «Пробелы спецификации»);
// реализован общий, протокол-независимый механизм комнат по воркспейсу и
// интерфейс Publisher, которым будут пользоваться домены T-027+ для
// публикации своих событий, не зная деталей транспорта.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
)

// Event — единица данных, публикуемая в комнату воркспейса. Форма нарочно
// минимальна (type + произвольный payload), т.к. конкретные типы событий
// (issue.created, workspace.updated, ...) определяют домены-издатели, а не
// этот пакет.
type Event struct {
	Type      string `json:"type"`
	Payload   any    `json:"payload,omitempty"`
}

// Publisher — интерфейс, которым домены публикуют realtime-события в канал
// пространства, не зависящий от транспорта. Реализация — *Hub.
type Publisher interface {
	Publish(workspaceID string, event Event)
}

// Hub держит активные WebSocket-подключения, сгруппированные по
// workspace_id ("комната" = один воркспейс).
type Hub struct {
	logger *slog.Logger

	mu    sync.RWMutex
	rooms map[string]map[*conn]struct{}
}

type conn struct {
	ws *websocket.Conn
}

// NewHub создаёт пустой хаб.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{logger: logger, rooms: make(map[string]map[*conn]struct{})}
}

func (h *Hub) join(workspaceID string, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[workspaceID]
	if !ok {
		room = make(map[*conn]struct{})
		h.rooms[workspaceID] = room
	}
	room[c] = struct{}{}
}

func (h *Hub) leave(workspaceID string, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[workspaceID]
	if !ok {
		return
	}
	delete(room, c)
	if len(room) == 0 {
		delete(h.rooms, workspaceID)
	}
}

// Publish рассылает событие всем подключениям комнаты workspaceID.
// Отправка best-effort: ошибка записи в одно соединение не должна мешать
// остальным, соединение просто будет закрыто отдельным read-loop'ом.
func (h *Hub) Publish(workspaceID string, event Event) {
	h.mu.RLock()
	room := h.rooms[workspaceID]
	conns := make([]*conn, 0, len(room))
	for c := range room {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("realtime: маршалинг события", "err", err)
		return
	}
	for _, c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		_ = c.ws.Write(ctx, websocket.MessageText, data)
		cancel()
	}
}

// Stats — снимок счётчиков для GET /health/realtime.
type Stats struct {
	Rooms       int `json:"rooms"`
	Connections int `json:"connections"`
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, room := range h.rooms {
		total += len(room)
	}
	return Stats{Rooms: len(h.rooms), Connections: total}
}
