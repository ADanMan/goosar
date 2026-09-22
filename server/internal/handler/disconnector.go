package handler

func (h *Handler) disconnectUser(userID, workspaceID string) int {
	if h.Disconnector != nil {
		return h.Disconnector.DisconnectUser(userID, workspaceID)
	}
	if h.Hub != nil {
		return h.Hub.DisconnectUser(userID, workspaceID)
	}
	return 0
}
