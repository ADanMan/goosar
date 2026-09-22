package realtime

const (
	ScopeWorkspace = "workspace"
	ScopeUser      = "user"
	ScopeTask      = "task"
	ScopeChat      = "chat"

	ScopeDaemonRuntime = "daemon_runtime"
)

type Broadcaster interface {
	BroadcastToScope(scopeType, scopeID string, message []byte)

	BroadcastToWorkspace(workspaceID string, message []byte)

	SendToUser(userID string, message []byte, excludeWorkspace ...string)

	Broadcast(message []byte)
}

type DaemonRuntimeDeliverer interface {
	DeliverDaemonRuntime(scopeID string, frame []byte, eventID string)
}

var _ Broadcaster = (*Hub)(nil)
