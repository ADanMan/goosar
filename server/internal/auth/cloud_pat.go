package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const PATPrefix = "gsl_"

const CloudPATPrefix = "gsln_"

const cloudPATCachePrefix = "mul:auth:mcn:"

const cloudPATCacheTTL = 60 * time.Second

const cloudPATVerifyPath = "/api/v1/pat/verify"

const cloudPATVerifyRequestMaxBytes = 4 * 1024

const cloudPATVerifyResponseMaxBytes = 64 * 1024

const cloudPATDefaultTimeout = 5 * time.Second

var (
	ErrCloudPATInvalid       = errors.New("cloud pat invalid")
	ErrCloudPATUnavailable   = errors.New("cloud pat verifier unavailable")
	ErrCloudPATNotConfigured = errors.New("cloud pat verifier not configured")
)

type CloudPATIdentity struct {
	OwnerID          string `json:"o"`
	InstanceID       string `json:"i"`
	InstanceRecordID string `json:"r"`
}

type CloudPATInvalidError struct {
	Reason string
}

func (e *CloudPATInvalidError) Error() string {
	if e == nil || e.Reason == "" {
		return "cloud pat invalid"
	}
	return "cloud pat invalid: " + e.Reason
}

func (e *CloudPATInvalidError) Is(target error) bool {
	return target == ErrCloudPATInvalid
}

const CloudPATInvalidReasonOwnerUnknown = "owner_unknown"

type OwnerLookupFunc func(ctx context.Context, ownerID string) (bool, error)

type CloudPATVerifier struct {
	baseURL string
	http    *http.Client
	rdb     *redis.Client
}

type CloudPATVerifierConfig struct {
	FleetBaseURL string

	HTTPClient *http.Client

	Redis *redis.Client
}

func NewCloudPATVerifier(cfg CloudPATVerifierConfig) *CloudPATVerifier {
	base := strings.TrimRight(strings.TrimSpace(cfg.FleetBaseURL), "/")
	if base == "" {
		return nil
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cloudPATDefaultTimeout}
	}
	return &CloudPATVerifier{
		baseURL: base,
		http:    client,
		rdb:     cfg.Redis,
	}
}

func (v *CloudPATVerifier) Configured() bool {
	return v != nil && v.baseURL != ""
}

func (v *CloudPATVerifier) Verify(ctx context.Context, token string, lookup OwnerLookupFunc) (CloudPATIdentity, error) {
	if v == nil || v.baseURL == "" {
		return CloudPATIdentity{}, ErrCloudPATNotConfigured
	}
	if token == "" {
		return CloudPATIdentity{}, ErrCloudPATInvalid
	}

	hash := HashToken(token)
	if id, ok := v.cacheGet(ctx, hash); ok {
		return id, nil
	}

	id, err := v.fetch(ctx, token)
	if err != nil {
		return CloudPATIdentity{}, err
	}

	if lookup != nil {
		exists, lookupErr := lookup(ctx, id.OwnerID)
		if lookupErr != nil {

			slog.Warn("cloud_pat: owner lookup failed; treating as unavailable", "error", lookupErr)
			return CloudPATIdentity{}, ErrCloudPATUnavailable
		}
		if !exists {

			slog.Warn("cloud_pat: cloud-verified owner_id has no local user", "owner_id", id.OwnerID)
			return CloudPATIdentity{}, &CloudPATInvalidError{Reason: CloudPATInvalidReasonOwnerUnknown}
		}
	}

	v.cacheSet(ctx, hash, id)
	return id, nil
}

type fleetVerifyRequest struct {
	Token string `json:"token"`
}

type fleetVerifyResponse struct {
	Valid            bool   `json:"valid"`
	Reason           string `json:"reason,omitempty"`
	OwnerID          string `json:"owner_id,omitempty"`
	InstanceID       string `json:"instance_id,omitempty"`
	InstanceRecordID string `json:"instance_record_id,omitempty"`
}

func (v *CloudPATVerifier) fetch(ctx context.Context, token string) (CloudPATIdentity, error) {
	body, err := json.Marshal(fleetVerifyRequest{Token: token})
	if err != nil {

		return CloudPATIdentity{}, fmt.Errorf("%w: marshal request: %v", ErrCloudPATUnavailable, err)
	}
	if len(body) > cloudPATVerifyRequestMaxBytes {

		return CloudPATIdentity{}, fmt.Errorf("%w: request body exceeds %d bytes", ErrCloudPATUnavailable, cloudPATVerifyRequestMaxBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+cloudPATVerifyPath, bytes.NewReader(body))
	if err != nil {
		return CloudPATIdentity{}, fmt.Errorf("%w: build request: %v", ErrCloudPATUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := v.http.Do(req)
	if err != nil {

		slog.Warn("cloud_pat: verify request failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {

		var snippet string
		if buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512)); len(buf) > 0 {
			snippet = strings.TrimSpace(string(buf))
		}
		slog.Warn("cloud_pat: verify returned non-200", "status", resp.StatusCode, "body", snippet)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, cloudPATVerifyResponseMaxBytes+1))
	if err != nil {
		slog.Warn("cloud_pat: read response failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}
	if len(raw) > cloudPATVerifyResponseMaxBytes {
		slog.Warn("cloud_pat: verify response too large", "limit", cloudPATVerifyResponseMaxBytes)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	var parsed fleetVerifyResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("cloud_pat: decode response failed", "error", err)
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	if !parsed.Valid {

		return CloudPATIdentity{}, &CloudPATInvalidError{Reason: parsed.Reason}
	}

	if parsed.OwnerID == "" {

		slog.Warn("cloud_pat: verify returned valid=true with empty owner_id")
		return CloudPATIdentity{}, ErrCloudPATUnavailable
	}

	return CloudPATIdentity{
		OwnerID:          parsed.OwnerID,
		InstanceID:       parsed.InstanceID,
		InstanceRecordID: parsed.InstanceRecordID,
	}, nil
}

func cloudPATCacheKey(hash string) string { return cloudPATCachePrefix + hash }

func (v *CloudPATVerifier) cacheGet(ctx context.Context, hash string) (CloudPATIdentity, bool) {
	if v == nil || v.rdb == nil {
		return CloudPATIdentity{}, false
	}
	raw, err := v.rdb.Get(ctx, cloudPATCacheKey(hash)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("cloud_pat: cache get failed; falling back to fleet", "error", err)
		}
		return CloudPATIdentity{}, false
	}
	var id CloudPATIdentity
	if err := json.Unmarshal(raw, &id); err != nil {
		slog.Warn("cloud_pat: cache entry malformed; falling back to fleet", "error", err)
		return CloudPATIdentity{}, false
	}
	if id.OwnerID == "" {

		return CloudPATIdentity{}, false
	}
	return id, true
}

func (v *CloudPATVerifier) cacheSet(ctx context.Context, hash string, id CloudPATIdentity) {
	if v == nil || v.rdb == nil {
		return
	}
	raw, err := json.Marshal(id)
	if err != nil {
		slog.Warn("cloud_pat: cache marshal failed", "error", err)
		return
	}
	if err := v.rdb.Set(ctx, cloudPATCacheKey(hash), raw, cloudPATCacheTTL).Err(); err != nil {
		slog.Warn("cloud_pat: cache set failed", "error", err)
	}
}
