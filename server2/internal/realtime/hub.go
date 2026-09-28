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
// минимальна (type + произвольный payload): конкретные типы событий
// (issue.created, workspace.updated, ...) определяют домены-издатели, а не
// этот пакет.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// Publisher — то, чем домены публикуют realtime-событие в канал
// пространства, не зная транспорта. Реализация — *Hub.
type Publisher interface {
	Publish(workspaceID string, event Event)
}

type socket struct{ ws *websocket.Conn }

// room — набор живых сокетов одного воркспейса, со своей блокировкой
// (комнаты воркспейсов не должны конкурировать друг с другом за один мьютекс
// хаба на каждую отправку).
type room struct {
	mu      sync.RWMutex
	sockets map[*socket]struct{}
}

func newRoom() *room { return &room{sockets: make(map[*socket]struct{})} }

func (rm *room) add(s *socket) {
	rm.mu.Lock()
	rm.sockets[s] = struct{}{}
	rm.mu.Unlock()
}

func (rm *room) remove(s *socket) (empty bool) {
	rm.mu.Lock()
	delete(rm.sockets, s)
	empty = len(rm.sockets) == 0
	rm.mu.Unlock()
	return empty
}

func (rm *room) snapshot() []*socket {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]*socket, 0, len(rm.sockets))
	for s := range rm.sockets {
		out = append(out, s)
	}
	return out
}

func (rm *room) size() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.sockets)
}

// Hub индексирует комнаты по workspace_id и рассылает Event всем сокетам
// нужной комнаты.
type Hub struct {
	logger *slog.Logger

	mu    sync.Mutex
	rooms map[string]*room
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{logger: logger, rooms: make(map[string]*room)}
}

func (h *Hub) join(workspaceID string, s *socket) {
	h.mu.Lock()
	rm, ok := h.rooms[workspaceID]
	if !ok {
		rm = newRoom()
		h.rooms[workspaceID] = rm
	}
	h.mu.Unlock()
	rm.add(s)
}

func (h *Hub) leave(workspaceID string, s *socket) {
	h.mu.Lock()
	rm, ok := h.rooms[workspaceID]
	h.mu.Unlock()
	if !ok {
		return
	}
	if rm.remove(s) {
		h.mu.Lock()
		if h.rooms[workspaceID] == rm && rm.size() == 0 {
			delete(h.rooms, workspaceID)
		}
		h.mu.Unlock()
	}
}

// Publish рассылает событие всем сокетам комнаты workspaceID. Best-effort:
// ошибка записи в один сокет не мешает остальным — этот сокет попросту
// закроется своим read-loop'ом отдельно.
func (h *Hub) Publish(workspaceID string, event Event) {
	h.mu.Lock()
	rm, ok := h.rooms[workspaceID]
	h.mu.Unlock()
	if !ok {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("realtime: не удалось сериализовать событие", "err", err, "type", event.Type)
		return
	}
	for _, s := range rm.snapshot() {
		writeCtx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		_ = s.ws.Write(writeCtx, websocket.MessageText, payload)
		cancel()
	}
}

// Stats — снимок счётчиков для GET /health/realtime.
type Stats struct {
	Rooms       int `json:"rooms"`
	Connections int `json:"connections"`
}

func (h *Hub) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, rm := range h.rooms {
		total += rm.size()
	}
	return Stats{Rooms: len(h.rooms), Connections: total}
}
