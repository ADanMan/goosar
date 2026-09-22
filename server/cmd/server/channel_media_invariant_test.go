package main

import (
	"testing"

	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	"github.com/adanman/goosar/server/internal/service"
)

func TestChannelMediaSettleDwarfsEveryPipelineBudget(t *testing.T) {
	budgets := map[string]int64{
		"engine media timeout (download+upload+bind budget)": int64(engine.DefaultMediaTimeout),
	}
	settle := int64(service.ChannelMediaReconcileSettleDelay)
	for name, budget := range budgets {
		if settle < 10*budget {
			t.Fatalf("settle delay %d must be >= 10x %s (%d)", settle, name, budget)
		}
	}
}
