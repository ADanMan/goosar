package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func SumAllCounters(m *BusinessMetrics) float64 {
	if m == nil {
		return 0
	}
	reg := prometheus.NewPedanticRegistry()
	for _, c := range m.Collectors() {

		reg.MustRegister(c)
	}
	families, err := reg.Gather()
	if err != nil {
		return 0
	}
	var total float64
	for _, fam := range families {
		if fam.GetType() != dto.MetricType_COUNTER {
			continue
		}
		for _, mtr := range fam.GetMetric() {
			if c := mtr.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

func GatherForTest(t *testing.T, m *BusinessMetrics) map[string]*dto.MetricFamily {
	t.Helper()
	if m == nil {
		t.Fatalf("GatherForTest: nil BusinessMetrics")
	}
	reg := prometheus.NewPedanticRegistry()
	for _, c := range m.Collectors() {
		reg.MustRegister(c)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("GatherForTest: gather failed: %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(families))
	for _, fam := range families {
		out[fam.GetName()] = fam
	}
	return out
}
