package metrics

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewReadinessCollectorNilCheck(t *testing.T) {
	if c := NewReadinessCollector(nil); c != nil {
		t.Fatalf("expected nil collector for nil check, got %#v", c)
	}
}

func TestReadinessCollectorGauges(t *testing.T) {
	cases := []struct {
		name      string
		ready     bool
		outOfDate bool
		want      string
	}{
		{
			name:  "ready",
			ready: true,
			want: `
# HELP goosar_migrations_out_of_date 1 when the database is missing migrations this binary requires, 0 otherwise.
# TYPE goosar_migrations_out_of_date gauge
goosar_migrations_out_of_date 0
# HELP goosar_ready 1 when the readiness check passes (same verdict as /readyz), 0 otherwise.
# TYPE goosar_ready gauge
goosar_ready 1
`,
		},
		{
			name:      "migrations out of date",
			ready:     false,
			outOfDate: true,
			want: `
# HELP goosar_migrations_out_of_date 1 when the database is missing migrations this binary requires, 0 otherwise.
# TYPE goosar_migrations_out_of_date gauge
goosar_migrations_out_of_date 1
# HELP goosar_ready 1 when the readiness check passes (same verdict as /readyz), 0 otherwise.
# TYPE goosar_ready gauge
goosar_ready 0
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewReadinessCollector(func(context.Context) (bool, bool) {
				return tc.ready, tc.outOfDate
			})
			reg := prometheus.NewRegistry()
			reg.MustRegister(c)
			if err := testutil.GatherAndCompare(reg, strings.NewReader(tc.want)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegistryRegistersReadinessWhenSupplied(t *testing.T) {
	reg := NewRegistry(RegistryOptions{
		Readiness: func(context.Context) (bool, bool) { return true, false },
	})
	families, err := reg.Gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range families {
		if f.GetName() == "goosar_ready" {
			found = true
		}
	}
	if !found {
		t.Fatal("goosar_ready is not exposed when RegistryOptions.Readiness is set")
	}
}
