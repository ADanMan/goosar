// Package realtime — хаб WebSocket-подключений клиента (/ws), протокол
// подписок (docs/50-api-contract.md §2.1: handshake, auth-фрейм,
// subscribe/unsubscribe/ping, комнаты workspace/user/task/chat,
// дедупликация по event_id) и интерфейс Publisher, которым домены T-027+
// публикуют свои события, не зная деталей транспорта.
package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Event — единица данных, публикуемая в комнату. Форма нарочно минимальна
// (type + произвольный payload плюс необязательные ActorType/ActorID —
// contract §2.1 "исходящие события... actor_id?, actor_type?"): конкретные
// типы событий (issue:created, workspace:updated, ...) определяют
// домены-издатели, а не этот пакет. EventID заполняется автоматически в
// Hub.publishRoom, если издатель его не задал — клиент дедуплицирует
// повторную доставку по нему (contract §2.1, окно 128 последних событий).
type Event struct {
	Type      string `json:"type"`
	Payload   any    `json:"payload,omitempty"`
	ActorType string `json:"actor_type,omitempty"`
	ActorID   string `json:"actor_id,omitempty"`
	EventID   string `json:"event_id,omitempty"`
}

// Publisher — то, чем домены публикуют realtime-событие в канал
// пространства, не зная транспорта. Реализация — *Hub. Публикация в комнаты
// user/task/chat — через отдельные методы Hub (PublishToUser/ToTask/ToChat),
// не часть этого узкого интерфейса (большинству доменов достаточно
// воркспейса).
type Publisher interface {
	Publish(workspaceID string, event Event)
}

// roomKey — комнаты хаба живут в одном пространстве имён, с префиксом по
// scope (contract §2.1: workspace/user/task/chat), а не в отдельных мапах —
// так подписка/отписка по произвольному scope+id (subscribe-фрейм) не
// требует знать заранее, какие scope вообще бывают.
func roomKey(scope, id string) string { return scope + ":" + id }

type socket struct {
	ws *websocket.Conn

	mu   sync.Mutex
	subs map[string]struct{} // занятые комнаты этого сокета (roomKey), для leaveAll при разрыве
}

func newSocket(ws *websocket.Conn) *socket {
	return &socket{ws: ws, subs: make(map[string]struct{})}
}

// room — набор живых сокетов одной комнаты (workspace:<id>, user:<id>,
// task:<id> или chat:<id>), со своей блокировкой (комнаты не должны
// конкурировать друг с другом за один мьютекс хаба на каждую отправку).
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

// Hub индексирует комнаты по ключу roomKey(scope, id) и рассылает Event всем
// сокетам нужной комнаты.
type Hub struct {
	logger *slog.Logger

	mu    sync.Mutex
	rooms map[string]*room

	connMu sync.Mutex
	conns  map[*socket]struct{} // все живые соединения — отдельно от rooms, потому что один сокет теперь состоит в нескольких комнатах (workspace+user+...) и счётчик Stats.Connections не должен задваиваться

	seq atomic.Uint64 // счётчик для генерации event_id, когда издатель его не задал
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{logger: logger, rooms: make(map[string]*room), conns: make(map[*socket]struct{})}
}

// registerConn/unregisterConn отмечают время жизни соединения независимо от
// того, в скольких комнатах оно состоит — вызываются один раз за апгрейд/
// разрыв в ws.go.
func (h *Hub) registerConn(s *socket) {
	h.connMu.Lock()
	h.conns[s] = struct{}{}
	h.connMu.Unlock()
}

func (h *Hub) unregisterConn(s *socket) {
	h.connMu.Lock()
	delete(h.conns, s)
	h.connMu.Unlock()
}

// join добавляет сокет в комнату key, регистрируя её в s.subs — leaveAll
// использует этот список, чтобы отписать сокет ровно от того, к чему он был
// подписан, при разрыве соединения.
func (h *Hub) join(key string, s *socket) {
	h.mu.Lock()
	rm, ok := h.rooms[key]
	if !ok {
		rm = newRoom()
		h.rooms[key] = rm
	}
	h.mu.Unlock()
	rm.add(s)

	s.mu.Lock()
	s.subs[key] = struct{}{}
	s.mu.Unlock()
}

func (h *Hub) leave(key string, s *socket) {
	h.mu.Lock()
	rm, ok := h.rooms[key]
	h.mu.Unlock()
	if !ok {
		return
	}
	if rm.remove(s) {
		h.mu.Lock()
		if h.rooms[key] == rm && rm.size() == 0 {
			delete(h.rooms, key)
		}
		h.mu.Unlock()
	}

	s.mu.Lock()
	delete(s.subs, key)
	s.mu.Unlock()
}

// leaveAll отписывает сокет от каждой комнаты, к которой он присоединился
// (workspace/user при апгрейде плюс любые task/chat из subscribe-фреймов) —
// вызывается один раз при разрыве соединения вместо парного leave на каждый
// join по месту вызова.
func (h *Hub) leaveAll(s *socket) {
	s.mu.Lock()
	keys := make([]string, 0, len(s.subs))
	for k := range s.subs {
		keys = append(keys, k)
	}
	s.mu.Unlock()
	for _, k := range keys {
		h.leave(k, s)
	}
}

// PublishRoom рассылает событие всем сокетам произвольной комнаты (см.
// roomKey). Best-effort: ошибка записи в один сокет не мешает остальным —
// этот сокет попросту закроется своим read-loop'ом отдельно. EventID
// заполняется здесь, если издатель его не задал.
func (h *Hub) PublishRoom(key string, event Event) {
	h.mu.Lock()
	rm, ok := h.rooms[key]
	h.mu.Unlock()
	if !ok {
		return
	}
	if event.EventID == "" {
		event.EventID = h.nextEventID()
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

func (h *Hub) nextEventID() string {
	return fmt.Sprintf("srv-%d-%d", time.Now().UnixNano(), h.seq.Add(1))
}

// Publish рассылает событие комнате workspace:<workspaceID> — реализует
// Publisher, точка входа для доменов, которым достаточно широковещания по
// воркспейсу (большинство).
func (h *Hub) Publish(workspaceID string, event Event) {
	h.PublishRoom(roomKey("workspace", workspaceID), event)
}

// PublishToUser/PublishToTask/PublishToChat — то же самое для комнат
// user:<id>/task:<id>/chat:<id> (contract §2.1: "Доставка... идёт через
// центральный диспетчер... комнаты workspace:<id>, user:<id>, task:<id>,
// chat:<id>"), для доменов, которым нужна более узкая адресация (например
// chat:message — только участникам конкретной чат-сессии).
func (h *Hub) PublishToUser(userID string, event Event) {
	h.PublishRoom(roomKey("user", userID), event)
}
func (h *Hub) PublishToTask(taskID string, event Event) {
	h.PublishRoom(roomKey("task", taskID), event)
}
func (h *Hub) PublishToChat(chatID string, event Event) {
	h.PublishRoom(roomKey("chat", chatID), event)
}

// CloseUserConnections принудительно закрывает все живые WebSocket-соединения
// пользователя (комната user:<userID>) — используется деактивацией/удалением
// учётной записи деплоя (T-029, internal/deployment): контракт требует
// «закрытие realtime-подключений» как побочный эффект deactivateDeploymentUser/
// deleteDeploymentUser. Возвращает число закрытых соединений. Закрытие само
// разбудит read-loop каждого сокета в ws.go, который и вызовет unregisterConn/
// leaveAll — здесь достаточно оборвать транспорт, не трогая счётчики хаба
// напрямую (иначе он рискует разойтись с реальным состоянием rooms/conns).
func (h *Hub) CloseUserConnections(userID string) int {
	h.mu.Lock()
	rm, ok := h.rooms[roomKey("user", userID)]
	h.mu.Unlock()
	if !ok {
		return 0
	}
	sockets := rm.snapshot()
	for _, s := range sockets {
		_ = s.ws.Close(websocket.StatusNormalClosure, "session revoked")
	}
	return len(sockets)
}

// Stats — снимок счётчиков для GET /health/realtime.
type Stats struct {
	Rooms       int `json:"rooms"`
	Connections int `json:"connections"`
}

func (h *Hub) Stats() Stats {
	h.mu.Lock()
	rooms := len(h.rooms)
	h.mu.Unlock()
	h.connMu.Lock()
	conns := len(h.conns)
	h.connMu.Unlock()
	return Stats{Rooms: rooms, Connections: conns}
}
