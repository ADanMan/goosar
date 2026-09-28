package task

import "encoding/json"

// rawFields разбирает тело частичного обновления (PUT.../PATCH...) как
// map[string]json.RawMessage — так домен различает "поле не передано" (ключа
// нет в мапе) от "поле передано как null" (значение — литерал "null"),
// что контракт явно требует для всех PUT/PATCH этого раздела (§1421
// "Общие правила валидации").
func rawFields(body json.RawMessage) (map[string]json.RawMessage, error) {
	if len(body) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func isJSONNull(raw json.RawMessage) bool { return string(raw) == "null" }

// stringField — present=false: ключ отсутствовал. present=true, value=nil:
// ключ был null. present=true, value=&s: ключ нёс строку.
func stringField(fields map[string]json.RawMessage, key string) (value *string, present bool, err error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	if isJSONNull(raw) {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, true, err
	}
	return &s, true, nil
}

func float64Field(fields map[string]json.RawMessage, key string) (value *float64, present bool, err error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	if isJSONNull(raw) {
		return nil, true, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, true, err
	}
	return &f, true, nil
}

func intField(fields map[string]json.RawMessage, key string) (value *int, present bool, err error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	if isJSONNull(raw) {
		return nil, true, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, true, err
	}
	return &n, true, nil
}

func boolField(fields map[string]json.RawMessage, key string) (value bool, present bool, err error) {
	raw, ok := fields[key]
	if !ok {
		return false, false, nil
	}
	if isJSONNull(raw) {
		return false, true, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, true, err
	}
	return b, true, nil
}
