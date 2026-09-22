package composio

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type APIError struct {
	HTTPStatus   int      `json:"-"`
	Message      string   `json:"message,omitempty"`
	Code         int      `json:"code,omitempty"`
	Slug         string   `json:"slug,omitempty"`
	Status       int      `json:"status,omitempty"`
	RequestID    string   `json:"request_id,omitempty"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	RawBody      []byte   `json:"-"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.HTTPStatus)
	}
	if e.Slug != "" {
		return fmt.Sprintf("composio: %d %s (%s)", e.HTTPStatus, msg, e.Slug)
	}
	return fmt.Sprintf("composio: %d %s", e.HTTPStatus, msg)
}

func (e *APIError) IsNotFound() bool { return e != nil && e.HTTPStatus == http.StatusNotFound }

func (e *APIError) IsUnauthorized() bool {
	return e != nil && e.HTTPStatus == http.StatusUnauthorized
}

func (e *APIError) IsRateLimited() bool {
	return e != nil && e.HTTPStatus == http.StatusTooManyRequests
}

func parseAPIError(status int, body []byte) *APIError {
	out := &APIError{HTTPStatus: status, RawBody: body}
	if len(body) == 0 {
		return out
	}
	var wire struct {
		Error APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {

		return out
	}
	wire.Error.HTTPStatus = status
	wire.Error.RawBody = body
	return &wire.Error
}
