package metrics

import (
	"fmt"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
)

const CostUSDTicksPerUSD = 10_000_000_000

type ModelPrice struct {
	Provider       string
	Model          string
	InputPerM      float64
	CacheReadPerM  float64
	CacheWritePerM float64
	OutputPerM     float64
}

var modelPricing = mustLoadModelPricing()

func mustLoadModelPricing() *runtimeregistry.ModelPricing {
	mp, err := runtimeregistry.LoadModelPricing()
	if err != nil {
		panic(fmt.Sprintf("metrics: load model pricing: %v", err))
	}
	return mp
}

func PriceForModelAlias(model string) (ModelPrice, bool) {
	entry, ok := modelPricing.PriceForAlias(model)
	if !ok {
		return ModelPrice{}, false
	}
	return ModelPrice{
		Provider:       entry.Provider,
		Model:          entry.Model,
		InputPerM:      entry.InputPerM,
		CacheReadPerM:  entry.CacheReadPerM,
		CacheWritePerM: entry.CacheWritePerM,
		OutputPerM:     entry.OutputPerM,
	}, true
}

func tokenCostUSD(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}
