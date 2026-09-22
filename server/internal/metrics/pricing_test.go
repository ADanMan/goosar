package metrics

import "testing"

func TestPriceForModelAliasAnthropicCurrentGeneration(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "claude-sonnet-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "anthropic:claude-sonnet-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "claude-5-sonnet",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "claude-fable-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10, CacheReadPerM: 1, CacheWritePerM: 12.5, OutputPerM: 50},
		},
		{
			model: "anthropic/claude-fable-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10, CacheReadPerM: 1, CacheWritePerM: 12.5, OutputPerM: 50},
		},
		{
			model: "claude-opus-4-8",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-opus-4.8", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 25},
		},

		{
			model: "claude-opus-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-opus-5", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 25},
		},
		{
			model: "anthropic/claude-opus-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-opus-5", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 25},
		},

		{
			model: "claude-opus-5[1m]",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-opus-5", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 25},
		},
	}

	for _, tc := range cases {
		got, ok := PriceForModelAlias(tc.model)
		if !ok {
			t.Fatalf("PriceForModelAlias(%q) did not resolve", tc.model)
		}
		if got != tc.want {
			t.Fatalf("PriceForModelAlias(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}
}

func TestPriceForModelAliasCodexGPT56(t *testing.T) {

	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "gpt-5.6-sol",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-sol", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 30},
		},
		{
			model: "openai:gpt-5.6-terra",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-terra", InputPerM: 2.5, CacheReadPerM: 0.25, CacheWritePerM: 3.125, OutputPerM: 15},
		},
		{
			model: "openai/gpt-5.6-luna",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-luna", InputPerM: 1, CacheReadPerM: 0.1, CacheWritePerM: 1.25, OutputPerM: 6},
		},
	}

	for _, tc := range cases {
		got, ok := PriceForModelAlias(tc.model)
		if !ok {
			t.Fatalf("PriceForModelAlias(%q) did not resolve", tc.model)
		}
		if got != tc.want {
			t.Fatalf("PriceForModelAlias(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}

	for _, model := range []string{
		"gpt-5.6-luna-pro",
		"gpt-5.6-luna/unknown",
		"gpt-5.6-sol-high",
		"gpt-5.6-mini",
		"gpt-5-6-luna",
		"gpt-5-6-sol",
		"gpt-5-6-terra",
	} {
		if got, ok := PriceForModelAlias(model); ok {
			t.Fatalf("PriceForModelAlias(%q) unexpectedly resolved to %+v; want unmapped", model, got)
		}
	}
}

func TestPriceForModelAliasGrok(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "xai:grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "xai/grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "grok-4.3",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.3", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-build-0.1",
			want:  ModelPrice{Provider: "xai", Model: "grok-build-0.1", InputPerM: 1, CacheReadPerM: 0.2, CacheWritePerM: 1, OutputPerM: 2},
		},
		{
			model: "grok-4.20-multi-agent-0309",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-multi-agent-0309", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-4.20-0309-reasoning",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-0309-reasoning", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-4.20-0309-non-reasoning",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-0309-non-reasoning", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
	}

	for _, tc := range cases {
		got, ok := PriceForModelAlias(tc.model)
		if !ok {
			t.Fatalf("PriceForModelAlias(%q) did not resolve", tc.model)
		}
		if got != tc.want {
			t.Fatalf("PriceForModelAlias(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}

	for _, model := range []string{
		"grok-composer-2.5-fast",
		"grok-composer-2.5",
		"grok-4.5-fast",
		"grok-4-5",
		"grok-4.20-0309",
		"grok",
		"unknown",
	} {
		if got, ok := PriceForModelAlias(model); ok {
			t.Fatalf("PriceForModelAlias(%q) unexpectedly resolved to %+v; want unmapped", model, got)
		}
	}
}

func TestGrokPricingMatchesRecordedTurn(t *testing.T) {

	const (
		uncachedInput = int64(2049)
		cacheRead     = int64(10880)
		output        = int64(29)
		wantUSD       = 75360000 / 1e10
	)

	price, ok := PriceForModelAlias("grok-4.5")
	if !ok {
		t.Fatal("grok-4.5 did not resolve")
	}
	got := tokenCostUSD(uncachedInput, price.InputPerM) +
		tokenCostUSD(cacheRead, price.CacheReadPerM) +
		tokenCostUSD(output, price.OutputPerM)

	if diff := got - wantUSD; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("recomputed cost = %.10f, want %.10f (xAI costUsdTicks)", got, wantUSD)
	}
}
