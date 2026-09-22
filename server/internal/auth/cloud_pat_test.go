package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fleetServerOpts struct {
	statusCode int
	body       string
	delay      time.Duration

	recordReqs *int32

	expectToken string
}

func newFleetServer(t *testing.T, opts fleetServerOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.recordReqs != nil {
			atomic.AddInt32(opts.recordReqs, 1)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/pat/verify" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if opts.expectToken != "" {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), opts.expectToken) {
				t.Errorf("request body missing expected token; got: %s", string(body))
			}
		}
		if opts.delay > 0 {
			time.Sleep(opts.delay)
		}
		status := opts.statusCode
		if status == 0 {
			status = http.StatusOK
		}
		body := opts.body
		if body == "" {
			body = `{
				"valid": true,
				"owner_id": "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001",
				"instance_id": "i-0123456789abcdef0",
				"instance_record_id": "01972f7e-8a13-72a1-bbb0-0874ed4e8e67"
			}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestCloudPATVerifier_NilSafe(t *testing.T) {
	var v *CloudPATVerifier
	if v.Configured() {
		t.Fatal("nil verifier reported Configured()=true")
	}
	_, err := v.Verify(context.Background(), "gsln_anything", nil)
	if !errors.Is(err, ErrCloudPATNotConfigured) {
		t.Fatalf("expected ErrCloudPATNotConfigured, got %v", err)
	}
}

func TestCloudPATVerifier_EmptyURLReturnsNil(t *testing.T) {
	if v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: "  "}); v != nil {
		t.Fatalf("expected nil for empty URL, got %#v", v)
	}
}

func TestCloudPATVerifier_VerifySuccess(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{expectToken: "gsln_test_token"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	if v == nil {
		t.Fatal("verifier should not be nil")
	}
	id, err := v.Verify(context.Background(), "gsln_test_token", nil)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if id.OwnerID != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
		t.Errorf("unexpected owner_id: %q", id.OwnerID)
	}
	if id.InstanceID != "i-0123456789abcdef0" {
		t.Errorf("unexpected instance_id: %q", id.InstanceID)
	}
	if id.InstanceRecordID != "01972f7e-8a13-72a1-bbb0-0874ed4e8e67" {
		t.Errorf("unexpected instance_record_id: %q", id.InstanceRecordID)
	}
}

func TestCloudPATVerifier_VerifyEmptyToken(t *testing.T) {
	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: "http://example.invalid"})
	_, err := v.Verify(context.Background(), "", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("expected ErrCloudPATInvalid, got %v", err)
	}
}

func TestCloudPATVerifier_InvalidReasons(t *testing.T) {
	reasons := []string{
		"format_invalid",
		"checksum_invalid",
		"token_not_found",
		"token_revoked",
		"token_expired",
		"owner_mismatch",
		"instance_mismatch",
	}
	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			body := `{"valid":false,"reason":"` + reason + `"}`
			srv := newFleetServer(t, fleetServerOpts{body: body})
			defer srv.Close()

			v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
			_, err := v.Verify(context.Background(), "gsln_x", nil)
			if !errors.Is(err, ErrCloudPATInvalid) {
				t.Fatalf("expected ErrCloudPATInvalid for reason %q, got %v", reason, err)
			}
			var typed *CloudPATInvalidError
			if !errors.As(err, &typed) {
				t.Fatalf("expected *CloudPATInvalidError, got %T", err)
			}
			if typed.Reason != reason {
				t.Errorf("expected Reason=%q, got %q", reason, typed.Reason)
			}
		})
	}
}

func TestCloudPATVerifier_FleetReturns500(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{statusCode: http.StatusInternalServerError, body: "boom"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for 500, got %v", err)
	}
}

func TestCloudPATVerifier_FleetReturns400(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{statusCode: http.StatusBadRequest, body: `{"error":"bad"}`})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for 400, got %v", err)
	}
}

func TestCloudPATVerifier_NetworkError(t *testing.T) {

	srv := newFleetServer(t, fleetServerOpts{})
	url := srv.URL
	srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{
		FleetBaseURL: url,
		HTTPClient:   &http.Client{Timeout: 200 * time.Millisecond},
	})
	_, err := v.Verify(context.Background(), "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable on network error, got %v", err)
	}
}

func TestCloudPATVerifier_ValidTrueWithoutOwnerIDFailsClosed(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{body: `{"valid":true}`})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for valid:true without owner_id, got %v", err)
	}
}

func TestCloudPATVerifier_DecodeError(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{body: "<not json>"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for decode error, got %v", err)
	}
}

func TestCloudPATVerifier_ContextCanceled(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{delay: 200 * time.Millisecond})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := v.Verify(ctx, "gsln_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable on canceled ctx, got %v", err)
	}
}

func TestCloudPATVerifier_TrimsTrailingSlash(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL + "/"})
	if v == nil {
		t.Fatal("verifier should not be nil")
	}
	if _, err := v.Verify(context.Background(), "gsln_x", nil); err != nil {
		t.Fatalf("Verify with trailing-slash baseURL failed: %v", err)
	}
}

func TestCloudPATVerifier_CacheHitSkipsHTTP(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &calls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	first, err := v.Verify(context.Background(), "gsln_repeat", nil)
	if err != nil {
		t.Fatalf("first Verify failed: %v", err)
	}
	if first.OwnerID == "" {
		t.Fatal("first Verify returned empty owner_id")
	}

	second, err := v.Verify(context.Background(), "gsln_repeat", nil)
	if err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if second != first {
		t.Fatalf("cache returned different identity: first=%+v second=%+v", first, second)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 fleet call, got %d", got)
	}
}

func TestCloudPATVerifier_NegativesNotCached(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{
		body:       `{"valid":false,"reason":"token_revoked"}`,
		recordReqs: &calls,
	})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	_, err := v.Verify(context.Background(), "gsln_revoked", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("first Verify: expected invalid, got %v", err)
	}
	_, err = v.Verify(context.Background(), "gsln_revoked", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("second Verify: expected invalid, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("negative result must not be cached; expected 2 fleet calls, got %d", got)
	}
}

func TestCloudPATVerifier_LookupRejectsUnknownOwner(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &calls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	lookup := func(_ context.Context, ownerID string) (bool, error) {

		if ownerID != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
			t.Errorf("lookup called with unexpected owner_id: %q", ownerID)
		}
		return false, nil
	}

	first, err := v.Verify(context.Background(), "gsln_unknown_owner", lookup)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("expected ErrCloudPATInvalid, got %v (id=%+v)", err, first)
	}
	var typed *CloudPATInvalidError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *CloudPATInvalidError, got %T", err)
	}
	if typed.Reason != CloudPATInvalidReasonOwnerUnknown {
		t.Errorf("expected reason=%q, got %q", CloudPATInvalidReasonOwnerUnknown, typed.Reason)
	}

	gotLookup := false
	lookupExists := func(_ context.Context, _ string) (bool, error) {
		gotLookup = true
		return true, nil
	}
	id, err := v.Verify(context.Background(), "gsln_unknown_owner", lookupExists)
	if err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if id.OwnerID == "" {
		t.Fatal("second Verify returned empty owner_id")
	}
	if !gotLookup {
		t.Fatal("second Verify did not consult the lookup — owner_unknown was wrongly cached")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("owner_unknown must not be cached; expected 2 fleet calls, got %d", got)
	}
}

func TestCloudPATVerifier_LookupErrorMapsToUnavailable(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	lookup := func(_ context.Context, _ string) (bool, error) {
		return false, errors.New("db is down")
	}
	_, err := v.Verify(context.Background(), "gsln_db_blip", lookup)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable, got %v", err)
	}
}

func TestCloudPATVerifier_LookupSuccessIsCached(t *testing.T) {
	rdb := newRedisTestClient(t)

	var fleetCalls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &fleetCalls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	var lookupCalls int32
	lookup := func(_ context.Context, _ string) (bool, error) {
		atomic.AddInt32(&lookupCalls, 1)
		return true, nil
	}

	if _, err := v.Verify(context.Background(), "gsln_cacheable", lookup); err != nil {
		t.Fatalf("first Verify failed: %v", err)
	}
	if _, err := v.Verify(context.Background(), "gsln_cacheable", lookup); err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if got := atomic.LoadInt32(&fleetCalls); got != 1 {
		t.Fatalf("expected 1 fleet call (second hits cache), got %d", got)
	}
	if got := atomic.LoadInt32(&lookupCalls); got != 1 {
		t.Fatalf("expected 1 lookup call (second hits cache), got %d", got)
	}
}
