package metrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	inventoryBeginMarker = "<!-- goosar-metric-inventory:begin -->"
	inventoryEndMarker   = "<!-- goosar-metric-inventory:end -->"
	inventoryDocPath     = "../../../SELF_HOSTING.md"
)

var misnamedGauges = map[string]bool{
	"goosar_agent_task_stuck_total": true,
	"goosar_workspace_total":        true,
}

type metricDoc struct {
	name   string
	typ    string
	labels string
	help   string
}

func (d metricDoc) row() string {
	return fmt.Sprintf("| `%s` | %s | %s | %s |", d.name, d.typ, d.labels, d.help)
}

func registryDescs(t *testing.T) []*prometheus.Desc {
	t.Helper()

	samplerPool, err := pgxpool.New(context.Background(), "postgres://goosar:goosar@127.0.0.1:1/goosar?sslmode=disable")
	if err != nil {
		t.Fatalf("build lazy sampler pool: %v", err)
	}
	t.Cleanup(samplerPool.Close)

	collectors := []prometheus.Collector{
		NewDBCollector(nil),
		NewRealtimeCollector(nil),
		NewDaemonWSCollector(nil),
		NewReadinessCollector(func(context.Context) (bool, bool) { return true, false }),
	}
	collectors = append(collectors, NewHTTPMetrics().Collectors()...)
	collectors = append(collectors, NewBusinessMetrics().Collectors()...)
	collectors = append(collectors, NewChannelMediaReconcilerMetrics().Collectors()...)
	collectors = append(collectors, NewBusinessSamplerCollector(&BusinessSamplerOptions{Pool: samplerPool}).Collectors()...)

	var descs []*prometheus.Desc
	for _, c := range collectors {
		ch := make(chan *prometheus.Desc, 512)
		go func(c prometheus.Collector) {
			c.Describe(ch)
			close(ch)
		}(c)
		for d := range ch {
			descs = append(descs, d)
		}
	}
	return descs
}

var descPattern = regexp.MustCompile(`fqName: "([^"]+)", help: "(.*)", constLabels: \{[^}]*\}, variableLabels: \{([^}]*)\}`)

func parseDesc(t *testing.T, desc *prometheus.Desc) (name, help, labels string) {
	t.Helper()
	m := descPattern.FindStringSubmatch(desc.String())
	if m == nil {
		t.Fatalf("cannot parse descriptor: %s", desc.String())
	}
	labels = strings.TrimSpace(m[3])
	if labels == "" {
		labels = "—"
	} else {
		parts := strings.Split(labels, ",")
		for i, p := range parts {
			parts[i] = "`" + strings.TrimSpace(p) + "`"
		}
		labels = strings.Join(parts, ", ")
	}

	return m[1], strings.ReplaceAll(m[2], "|", `\|`), labels
}

func TestMetricInventoryIsDocumented(t *testing.T) {
	docPath, err := filepath.Abs(inventoryDocPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	begin := strings.Index(doc, inventoryBeginMarker)
	end := strings.Index(doc, inventoryEndMarker)
	if begin < 0 || end < 0 || end < begin {
		t.Fatalf("%s must contain %s ... %s", docPath, inventoryBeginMarker, inventoryEndMarker)
	}
	section := doc[begin+len(inventoryBeginMarker) : end]

	documented := map[string]string{}
	rowPattern := regexp.MustCompile("^\\| `(goosar_[a-z0-9_]+)` \\| ([a-z]+) \\|")
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimRight(line, " ")
		m := rowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if _, dup := documented[m[1]]; dup {
			t.Errorf("%s is documented twice", m[1])
		}
		documented[m[1]] = line
		if strings.HasSuffix(m[1], "_total") && m[2] != "counter" && !misnamedGauges[m[1]] {
			t.Errorf("%s ends in _total but is documented as %q", m[1], m[2])
		}
	}

	expected := map[string]metricDoc{}
	for _, desc := range registryDescs(t) {
		name, help, labels := parseDesc(t, desc)
		expected[name] = metricDoc{name: name, labels: labels, help: help}
	}

	expected["goosar_build_info"] = metricDoc{
		name:   "goosar_build_info",
		labels: "`version`, `commit`",
		help:   "Build information for the Goosar server binary.",
	}

	var missing, stale []string
	for name, want := range expected {
		got, ok := documented[name]
		if !ok {
			want.typ = "<counter|gauge|histogram>"
			missing = append(missing, want.row())
			continue
		}

		typ := regexp.MustCompile("^\\| `[a-z0-9_]+` \\| ([a-z]+) \\|").FindStringSubmatch(got)[1]
		want.typ = typ
		if got != want.row() {
			t.Errorf("inventory row for %s is stale.\n  doc:  %s\n  code: %s", name, got, want.row())
		}
	}
	for name := range documented {
		if _, ok := expected[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	if len(missing) > 0 {
		t.Errorf("SELF_HOSTING.md metric inventory is missing %d metric(s). Add:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("SELF_HOSTING.md documents metrics the registry no longer exports: %s",
			strings.Join(stale, ", "))
	}
}
