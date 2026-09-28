package httpapi

import (
	"encoding/json"
	"net/http"
)

// Error — тело ошибки по контракту (components/schemas/Error): поле error
// обязательно, code — стабильный машинный код (может отсутствовать), request_id
// эхом отдаёт X-Request-ID, если он уже был выставлен на этом ответе.
type Error struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteJSON пишет v как JSON с заданным статусом.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError пишет тело ошибки контракта. requestID берётся из заголовка
// ответа X-Request-ID, если он был выставлен middleware.
func WriteError(w http.ResponseWriter, status int, message, code string) {
	WriteJSON(w, status, Error{
		Error:     message,
		Code:      code,
		RequestID: w.Header().Get("X-Request-ID"),
	})
}

// WriteNotImplemented — тело для заглушек 406 операций контракта, ещё не
// реализованных ни одним доменом.
func WriteNotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, http.StatusNotImplemented, "not implemented", "not_implemented")
}

// DecodeJSON декодирует тело запроса в v; на пустое тело (allowEmpty) не
// ругается, оставляя v в нулевом значении.
func DecodeJSON(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

// BadRequest — короткий хелпер для 400 с кодом invalid_request.
func BadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, message, "invalid_request")
}

// Unauthorized — короткий хелпер для 401.
func Unauthorized(w http.ResponseWriter, message string) {
	if message == "" {
		message = "not authenticated"
	}
	WriteError(w, http.StatusUnauthorized, message, "unauthorized")
}

// Forbidden — короткий хелпер для 403.
func Forbidden(w http.ResponseWriter, message string) {
	if message == "" {
		message = "insufficient permissions"
	}
	WriteError(w, http.StatusForbidden, message, "forbidden")
}

// NotFound — короткий хелпер для 404.
func NotFound(w http.ResponseWriter, message string) {
	if message == "" {
		message = "not found"
	}
	WriteError(w, http.StatusNotFound, message, "not_found")
}
