package util

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func ParseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, fmt.Errorf("invalid UUID %q: %w", s, err)
	}
	if !u.Valid {
		return u, fmt.Errorf("invalid UUID: %q", s)
	}
	return u, nil
}

func MustParseUUID(s string) pgtype.UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

func UUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

func TextToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func PtrToText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func StrToText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func TimestampToString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func TimestampToPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339)
	return &s
}

func DateToPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format(time.DateOnly)
	return &s
}

func ParseCalendarDate(s string) (pgtype.Date, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return pgtype.Date{Time: t, Valid: true}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
			return pgtype.Date{
				Time:  time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC),
				Valid: true,
			}, nil
		}
		return pgtype.Date{}, fmt.Errorf("invalid date %q: timestamps must be a UTC midnight boundary (e.g. 2026-03-01T00:00:00Z); use YYYY-MM-DD", s)
	}
	return pgtype.Date{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
}

func UUIDToPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := UUIDToString(u)
	return &s
}

func Int8ToPtr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func Int4ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func PtrToInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}
