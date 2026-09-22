package runtimeregistry

import "testing"

func TestLoadModelPricingRoundTrip(t *testing.T) {
	mp, err := LoadModelPricing()
	if err != nil {
		t.Fatalf("LoadModelPricing: %v", err)
	}

	cases := []struct {
		key  string
		want ModelPriceEntry
	}{
		{
			key: "anthropic:claude-sonnet-5",
			want: ModelPriceEntry{
				Key: "anthropic:claude-sonnet-5", Provider: "anthropic", Model: "claude-sonnet-5",
				InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.50, OutputPerM: 10.00,
			},
		},
		{
			key: "openai:gpt-5.6-terra",
			want: ModelPriceEntry{
				Key: "openai:gpt-5.6-terra", Provider: "openai", Model: "gpt-5.6-terra",
				InputPerM: 2.50, CacheReadPerM: 0.25, CacheWritePerM: 3.125, OutputPerM: 15.00,
			},
		},
		{
			key: "deepseek:v4-pro",
			want: ModelPriceEntry{
				Key: "deepseek:v4-pro", Provider: "deepseek", Model: "v4-pro",
				InputPerM: 1.74, CacheReadPerM: 0.0145, CacheWritePerM: 1.74, OutputPerM: 3.48,
			},
		},
		{
			key: "xai:grok-4.5",
			want: ModelPriceEntry{
				Key: "xai:grok-4.5", Provider: "xai", Model: "grok-4.5",
				InputPerM: 2.00, CacheReadPerM: 0.30, CacheWritePerM: 2.00, OutputPerM: 6.00,
			},
		},
	}

	for _, tc := range cases {
		got, ok := mp.byKey[tc.key]
		if !ok {
			t.Fatalf("byKey[%q] missing", tc.key)
		}
		if got != tc.want {
			t.Fatalf("byKey[%q] = %+v, want %+v", tc.key, got, tc.want)
		}
	}

	got, ok := mp.PriceForAlias("claude-5-sonnet")
	if !ok {
		t.Fatal("PriceForAlias(claude-5-sonnet) did not resolve")
	}
	want := ModelPriceEntry{
		Key: "anthropic:claude-sonnet-5", Provider: "anthropic", Model: "claude-sonnet-5",
		InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.50, OutputPerM: 10.00,
	}
	if got != want {
		t.Fatalf("PriceForAlias(claude-5-sonnet) = %+v, want %+v", got, want)
	}

	if len(mp.byKey) != 32 {
		t.Fatalf("got %d price rows, want 32", len(mp.byKey))
	}
	if len(mp.rules) != 32 {
		t.Fatalf("got %d alias rules, want 32", len(mp.rules))
	}
}
