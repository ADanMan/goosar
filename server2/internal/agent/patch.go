package agent

import "encoding/json"

// fieldSet — тело PUT/PATCH как map "ключ -> сырое значение JSON", чтобы
// отличать "поле не передано" (ключа нет) от "поле передано как null"
// (contract §11 "Общие правила валидации"). decodeField использует дженерики
// вместо набора типизированных функций на каждый тип поля.
type fieldSet map[string]json.RawMessage

func parseFields(body []byte) (fieldSet, error) {
	if len(body) == 0 {
		return fieldSet{}, nil
	}
	var m fieldSet
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (f fieldSet) has(key string) bool { _, ok := f[key]; return ok }

// decodeField разбирает f[key] в v; present=true, если ключ был в теле (даже
// если его значение — null, тогда v не трогается вызывающим кодом).
func decodeField[T any](f fieldSet, key string, v *T) (present bool, err error) {
	raw, ok := f[key]
	if !ok {
		return false, nil
	}
	if string(raw) == "null" {
		return true, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return true, err
	}
	return true, nil
}
