package realtime

import "github.com/oklog/ulid/v2"

const EventConnectionRevoked = "connection:revoked"

var revokedFrame = []byte(`{"type":"` + EventConnectionRevoked + `"}`)

type UserDisconnector interface {
	DisconnectUser(userID, workspaceID string) int
}

type RelayDisconnector struct {
	hub   *Hub
	relay RelayPublisher
}

func NewRelayDisconnector(hub *Hub, relay RelayPublisher) *RelayDisconnector {
	return &RelayDisconnector{hub: hub, relay: relay}
}

func (d *RelayDisconnector) DisconnectUser(userID, workspaceID string) int {
	if userID == "" {
		return 0
	}
	closed := d.hub.DisconnectUser(userID, workspaceID)

	_ = d.relay.PublishWithID(ScopeUser, userID, workspaceID, revokedFrame, ulid.Make().String())
	return closed
}

var (
	_ UserDisconnector = (*Hub)(nil)
	_ UserDisconnector = (*RelayDisconnector)(nil)
)
