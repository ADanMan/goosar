package featureflag

import (
	"context"
	"testing"
)

func TestEvalContextLookup(t *testing.T) {
	t.Parallel()
	ec := EvalContext{
		UserID:      "u-1",
		WorkspaceID: "w-2",
		Attributes:  map[string]string{"plan": "pro", "country": ""},
	}
	tests := []struct {
		name  string
		key   string
		value string
		found bool
	}{
		{"user_id", "user_id", "u-1", true},
		{"workspace_id", "workspace_id", "w-2", true},
		{"plan", "plan", "pro", true},
		{"empty attribute treated as missing", "country", "", false},
		{"unknown attribute", "unknown", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := ec.Lookup(tt.key)
			if v != tt.value || ok != tt.found {
				t.Fatalf("Lookup(%q) = (%q, %v), want (%q, %v)", tt.key, v, ok, tt.value, tt.found)
			}
		})
	}
}

func TestEvalContextRoundTripThroughContext(t *testing.T) {
	t.Parallel()
	ec := EvalContext{UserID: "u-1"}
	ctx := WithEvalContext(context.Background(), ec)
	got := EvalContextFrom(ctx)
	if got.UserID != "u-1" {
		t.Fatalf("EvalContext did not round-trip, got %+v", got)
	}
}

func TestEvalContextFromUnattachedContext(t *testing.T) {
	t.Parallel()

	got := EvalContextFrom(context.Background())
	if got.UserID != "" || got.WorkspaceID != "" || got.Attributes != nil {
		t.Fatalf("unattached context should yield zero EvalContext, got %+v", got)
	}
}

func TestEvalContextFromNilContext(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // deliberately exercise the nil-ctx defensive path.
	got := EvalContextFrom(nil)
	if got.UserID != "" {
		t.Fatalf("nil context must yield zero EvalContext, got %+v", got)
	}
}

func TestPercentBucketStable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key, id string
		want    int
	}{
		{"feature_a", "user-1", bucketFor("feature_a", "user-1")},
		{"feature_b", "", bucketFor("feature_b", "")},
	}
	for _, tc := range cases {
		got := bucketFor(tc.key, tc.id)
		if got != tc.want {
			t.Fatalf("bucketFor(%q, %q) = %d, want %d", tc.key, tc.id, got, tc.want)
		}
		if got < 0 || got >= 100 {
			t.Fatalf("bucket out of range: %d", got)
		}
	}
}

func TestPercentBucketSeparator(t *testing.T) {
	t.Parallel()

	left := bucketFor("ab", "c")
	right := bucketFor("a", "bc")
	if left == right {

		t.Fatalf("hash separator failed: bucketFor('ab','c') == bucketFor('a','bc') == %d", left)
	}
}

func TestPercentBucketCrossLanguageGolden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key, id string
		want    int
	}{

		{"billing_new_invoice", "user-42", 97},
		{"feature_a", "user-1", 50},
		{"checkout_algo", "u-7f8a", 11},
		{"ws_rollout", "workspace-1", 62},
		{"empty_id_flag", "", 83},

		{"flag", "é", 53},
		{"flag", "🦄", 82},
		{"实验", "user-1", 90},
		{"flag", "用户-1", 95},
		{"checkout_算法", "user-100", 79},
	}
	for _, tc := range cases {
		got := bucketFor(tc.key, tc.id)
		if got != tc.want {
			t.Fatalf(
				"cross-language golden mismatch: bucketFor(%q, %q) = %d, want %d. "+
					"If you changed the hash you MUST also update hash.test.ts.",
				tc.key, tc.id, got, tc.want,
			)
		}
	}
}
