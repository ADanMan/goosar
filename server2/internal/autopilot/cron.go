// cron.go — свой разбор стандартных 5-полевых cron-выражений (минута час
// день-месяца месяц день-недели), без внешней библиотеки (правило ADR
// 0001-stack.md: robfig/cron исключён явно правилами тикета T-028, и раз уж
// пишем сами — gronx как дополнительная зависимость тоже не нужен). Формат —
// ровно тот, что описывает contract §7 ("cron + таймзона") и
// previewAutopilotCron/createAutopilotTrigger: 5 полей, `*`, списки через
// запятую, диапазоны `A-B`, шаг `*/N` и `A-B/N`. Именованные месяцы/дни
// недели (JAN/MON) не поддерживаются — контракт не требует их (cron-preview
// в contract_test.go шлёт только числовые выражения вида "0 9 * * *").
package autopilot

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fieldRange — допустимые границы одного из 5 полей.
type fieldRange struct{ min, max int }

var fieldRanges = [5]fieldRange{
	{0, 59}, // минута
	{0, 23}, // час
	{1, 31}, // день месяца
	{1, 12}, // месяц
	{0, 7},  // день недели (0 и 7 — оба воскресенье)
}

// Schedule — разобранное cron-выражение: каждое поле — множество допустимых
// значений (bitset как map[int]bool — поля маленькие, до 60 элементов,
// простота важнее байта экономии).
type Schedule struct {
	minute, hour, dom, month, dow map[int]bool
	domStar, dowStar              bool // "*" в этих двух полях — особый случай OR ниже (домStar=true — учитывается только dow, и наоборот)
	raw                           string
}

// ErrInvalidCron — expr не разбирается как 5-полевое cron-выражение.
type ErrInvalidCron struct{ Reason string }

func (e *ErrInvalidCron) Error() string {
	return "autopilot: невалидное cron-выражение: " + e.Reason
}

// ParseCron разбирает expr в Schedule. Правило пересечения
// day-of-month/day-of-week — классическое cron-поведение: если оба поля
// ограничены (не "*"), срабатывание — по OR (день месяца ИЛИ день недели
// подходит), а не по AND; если хотя бы одно — "*", учитывается только другое.
func ParseCron(expr string) (Schedule, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return Schedule{}, &ErrInvalidCron{Reason: fmt.Sprintf("нужно 5 полей, получено %d", len(fields))}
	}
	sets := make([]map[int]bool, 5)
	stars := make([]bool, 5)
	for i, f := range fields {
		set, isStar, err := parseField(f, fieldRanges[i])
		if err != nil {
			return Schedule{}, err
		}
		sets[i], stars[i] = set, isStar
	}
	return Schedule{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domStar: stars[2], dowStar: stars[4], raw: expr,
	}, nil
}

func parseField(f string, r fieldRange) (set map[int]bool, isStar bool, err error) {
	set = make(map[int]bool)
	if f == "*" {
		for v := r.min; v <= r.max; v++ {
			set[v] = true
		}
		return set, true, nil
	}
	for _, part := range strings.Split(f, ",") {
		if part == "" {
			return nil, false, &ErrInvalidCron{Reason: "пустой элемент списка"}
		}
		base, step := part, 1
		if idx := strings.IndexByte(part, '/'); idx >= 0 {
			base = part[:idx]
			stepVal, convErr := strconv.Atoi(part[idx+1:])
			if convErr != nil || stepVal <= 0 {
				return nil, false, &ErrInvalidCron{Reason: "неверный шаг в " + part}
			}
			step = stepVal
		}
		lo, hi := r.min, r.max
		if base != "*" {
			if dash := strings.IndexByte(base, '-'); dash >= 0 {
				loVal, err1 := strconv.Atoi(base[:dash])
				hiVal, err2 := strconv.Atoi(base[dash+1:])
				if err1 != nil || err2 != nil || loVal > hiVal {
					return nil, false, &ErrInvalidCron{Reason: "неверный диапазон " + base}
				}
				lo, hi = loVal, hiVal
			} else {
				v, convErr := strconv.Atoi(base)
				if convErr != nil {
					return nil, false, &ErrInvalidCron{Reason: "не число: " + base}
				}
				lo, hi = v, v
			}
		}
		if lo < r.min || hi > r.max {
			return nil, false, &ErrInvalidCron{Reason: fmt.Sprintf("значение вне диапазона [%d,%d]: %s", r.min, r.max, part)}
		}
		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}
	return set, false, nil
}

// maxScanYears — предохранитель от бесконечного цикла на заведомо
// невыполнимом выражении (например "0 0 30 2 *" — 30 февраля не существует
// никогда); контракт не требует диагностировать это отдельно, но зависать
// сервер тоже не должен.
const maxScanYears = 8

// Next возвращает следующий момент срабатывания строго после from (from
// понимается в часовом поясе, уже выставленном на нём вызывающим — сам
// Schedule часовых поясов не знает, см. NextInLocation).
func (s Schedule) Next(from time.Time) (time.Time, bool) {
	t := from.Truncate(time.Minute).Add(time.Minute)
	deadline := from.AddDate(maxScanYears, 0, 0)
	for t.Before(deadline) {
		if s.month[int(t.Month())] && s.dayMatches(t) {
			if s.hour[t.Hour()] && s.minute[t.Minute()] {
				return t, true
			}
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

// dayMatches — правило OR/AND day-of-month vs day-of-week, описанное в
// ParseCron.
func (s Schedule) dayMatches(t time.Time) bool {
	domOK := s.dom[t.Day()]
	dow := int(t.Weekday()) // time.Sunday == 0, совпадает с cron
	dowOK := s.dow[dow] || (dow == 0 && s.dow[7])
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowOK
	case s.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}

// NextInLocation — Next, но from и результат переводятся в loc (IANA-имя
// часового пояса триггера, contract: "cron + таймзона"). Пустой loc — UTC.
func (s Schedule) NextInLocation(from time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	next, ok := s.Next(from.In(loc))
	if !ok {
		return time.Time{}, false
	}
	return next.UTC(), true
}

// LoadTimezone разбирает IANA-имя таймзоны (пусто/"UTC" — time.UTC).
func LoadTimezone(tz string) (*time.Location, error) {
	if tz == "" || strings.EqualFold(tz, "UTC") {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, &ErrInvalidTimezone{Reason: err.Error()}
	}
	return loc, nil
}

// ErrInvalidTimezone — tz не распознан как имя IANA-таймзоны.
type ErrInvalidTimezone struct{ Reason string }

func (e *ErrInvalidTimezone) Error() string {
	return "autopilot: невалидная таймзона: " + e.Reason
}
