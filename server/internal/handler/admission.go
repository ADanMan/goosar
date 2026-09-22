package handler

import (
	"net/http"

	"github.com/adanman/goosar/server/internal/dispatch"
)

type DispatchStatus string

const (
	DispatchQueued DispatchStatus = "queued"

	DispatchCoalesced DispatchStatus = "coalesced"

	DispatchDeferred DispatchStatus = "deferred"

	DispatchBlocked DispatchStatus = "blocked"
)

type DispatchReasonCode = dispatch.ReasonCode

const (
	ReasonQueued                = dispatch.ReasonQueued
	ReasonCoalesced             = dispatch.ReasonCoalesced
	ReasonDeferred              = dispatch.ReasonDeferred
	ReasonInvocationNotAllowed  = dispatch.ReasonInvocationNotAllowed
	ReasonTargetUnavailable     = dispatch.ReasonTargetUnavailable
	ReasonRuntimeOffline        = dispatch.ReasonRuntimeOffline
	ReasonAttributionBlocked    = dispatch.ReasonAttributionBlocked
	ReasonAlreadyActive         = dispatch.ReasonAlreadyActive
	ReasonSelfTriggerSuppressed = dispatch.ReasonSelfTriggerSuppressed
	ReasonInternalError         = dispatch.ReasonInternalError
)

type DispatchTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type DispatchOutcome struct {
	Status     DispatchStatus     `json:"status"`
	ReasonCode DispatchReasonCode `json:"reason_code"`
	Target     *DispatchTarget    `json:"target,omitempty"`
	TaskID     *string            `json:"task_id,omitempty"`
	RunID      *string            `json:"run_id,omitempty"`
}

type dispatchBlockedResponse struct {
	Error      string             `json:"error"`
	ReasonCode DispatchReasonCode `json:"reason_code"`
}

func (h *Handler) writeDispatchBlocked(w http.ResponseWriter, status int, code DispatchReasonCode) {
	writeJSON(w, status, dispatchBlockedResponse{
		Error:      dispatchBlockedFallbackMessage(code),
		ReasonCode: code,
	})
}

func dispatchBlockedFallbackMessage(code DispatchReasonCode) string {
	switch code {
	case ReasonInvocationNotAllowed:
		return "you don't have permission to use this target"
	case ReasonTargetUnavailable:
		return "the target is unavailable"
	case ReasonRuntimeOffline:
		return "the target's runtime is offline"
	case ReasonAttributionBlocked:
		return "the run couldn't be attributed to a responsible member"
	case ReasonAlreadyActive:
		return "a run is already active for this target"
	default:
		return "the run was blocked"
	}
}
