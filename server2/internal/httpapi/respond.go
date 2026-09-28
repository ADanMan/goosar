package httpapi

import (
	"encoding/json"
	"io"
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

// WriteJSON сериализует v как тело ответа с заданным статусом; v == nil
// пишет только статус, без тела (для 204-подобных случаев через этот же путь).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError пишет тело ошибки контракта, подхватывая X-Request-ID, если
// middleware уже выставил его на этом ответе.
func WriteError(w http.ResponseWriter, status int, message, code string) {
	WriteJSON(w, status, Error{Error: message, Code: code, RequestID: w.Header().Get("X-Request-ID")})
}

// WriteNotImplemented — общее тело для 501-заглушек ещё не освоенных доменом
// операций контракта.
func WriteNotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, http.StatusNotImplemented, "not implemented", "not_implemented")
}

// DecodeJSON декодирует тело запроса в v. Пустое тело — не ошибка, v просто
// остаётся в нулевом значении (многие PATCH-эндпоинты контракта не требуют тела).
func DecodeJSON(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}

// ReadBody читает тело запроса целиком как []byte (пустое тело — []byte(nil),
// не ошибка). В отличие от DecodeJSON, оставляет вызывающему сами байты —
// нужно, когда одно и то же тело разбирается дважды разными способами
// (например строгий DTO плюс "какие ключи вообще присутствовали" для
// различения "поле не передано" от "поле передано как null", см.
// internal/task/patch.go).
func ReadBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil
	}
	return io.ReadAll(r.Body)
}

// statusHelper фиксирует пару (код статуса, дефолтное сообщение, код ошибки)
// для четырёх самых частых 4xx контракта — четыре публичные функции ниже
// делегируют сюда, вместо того чтобы каждая по отдельности дублировала
// проверку "message == "" ? дефолт : message".
type statusHelper struct {
	status   int
	code     string
	fallback string
}

func (h statusHelper) write(w http.ResponseWriter, message string) {
	if message == "" {
		message = h.fallback
	}
	WriteError(w, h.status, message, h.code)
}

var (
	badRequestHelper   = statusHelper{http.StatusBadRequest, "invalid_request", "invalid request"}
	unauthorizedHelper = statusHelper{http.StatusUnauthorized, "unauthorized", "not authenticated"}
	forbiddenHelper    = statusHelper{http.StatusForbidden, "forbidden", "insufficient permissions"}
	notFoundHelper     = statusHelper{http.StatusNotFound, "not_found", "not found"}
)

func BadRequest(w http.ResponseWriter, message string)   { badRequestHelper.write(w, message) }
func Unauthorized(w http.ResponseWriter, message string) { unauthorizedHelper.write(w, message) }
func Forbidden(w http.ResponseWriter, message string)    { forbiddenHelper.write(w, message) }
func NotFound(w http.ResponseWriter, message string)     { notFoundHelper.write(w, message) }
