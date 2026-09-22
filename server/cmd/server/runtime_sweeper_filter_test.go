package main

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type fakeLiveness struct {
	available   bool
	aliveResult map[string]bool
	aliveOK     bool
}

func (f *fakeLiveness) Available() bool { return f.available }
func (f *fakeLiveness) Touch(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeLiveness) IsAliveBatch(_ context.Context, ids []string) (map[string]bool, bool) {
	if !f.aliveOK {
		return nil, false
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = f.aliveResult[id]
	}
	return out, true
}
func (f *fakeLiveness) Forget(_ context.Context, _ string) {}

func makeUUIDForFilter(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}

func candidateRow(t *testing.T, id string) db.SelectStaleOnlineRuntimesRow {
	return db.SelectStaleOnlineRuntimesRow{ID: makeUUIDForFilter(t, id)}
}

func sortedIDStrings(ids []pgtype.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = util.UUIDToString(id)
	}
	sort.Strings(out)
	return out
}

func TestFilterStaleRuntimesByLiveness_NoopStorePassesThrough(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	candidates := []db.SelectStaleOnlineRuntimesRow{
		candidateRow(t, a),
		candidateRow(t, b),
	}

	got := filterStaleRuntimesByLiveness(context.Background(), candidates, handler.NewNoopLivenessStore())

	want := []string{a, b}
	if !reflect.DeepEqual(sortedIDStrings(got), want) {
		t.Fatalf("noop store should pass every candidate through: got=%v want=%v",
			sortedIDStrings(got), want)
	}
}

func TestFilterStaleRuntimesByLiveness_AliveCandidatesSkipped(t *testing.T) {
	aliveID := "11111111-1111-1111-1111-111111111111"
	deadID := "22222222-2222-2222-2222-222222222222"
	candidates := []db.SelectStaleOnlineRuntimesRow{
		candidateRow(t, aliveID),
		candidateRow(t, deadID),
	}

	got := filterStaleRuntimesByLiveness(context.Background(), candidates, &fakeLiveness{
		available: true,
		aliveOK:   true,
		aliveResult: map[string]bool{
			aliveID: true,
		},
	})

	want := []string{deadID}
	if !reflect.DeepEqual(sortedIDStrings(got), want) {
		t.Fatalf("alive candidates should be skipped: got=%v want=%v",
			sortedIDStrings(got), want)
	}
}

func TestFilterStaleRuntimesByLiveness_StoreErrorFallsBackToDB(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	candidates := []db.SelectStaleOnlineRuntimesRow{
		candidateRow(t, a),
		candidateRow(t, b),
	}

	got := filterStaleRuntimesByLiveness(context.Background(), candidates, &fakeLiveness{
		available: true,
		aliveOK:   false,
	})

	want := []string{a, b}
	if !reflect.DeepEqual(sortedIDStrings(got), want) {
		t.Fatalf("store error should fall back to passing every candidate: got=%v want=%v",
			sortedIDStrings(got), want)
	}
}
