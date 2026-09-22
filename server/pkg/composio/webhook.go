package composio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
)

const DefaultWebhookTolerance = 300 * time.Second

var (
	ErrMissingWebhookHeaders   = errors.New("composio: missing webhook headers")
	ErrInvalidWebhookSignature = errors.New("composio: invalid webhook signature")
	ErrWebhookTimestampStale   = errors.New("composio: webhook timestamp outside tolerance")
	ErrWebhookSecretMissing    = errors.New("composio: webhook secret is empty")
)

type WebhookHeaders struct {
	ID        string
	Timestamp string
	Signature string
}

func HeadersFromHTTP(h http.Header) WebhookHeaders {
	return WebhookHeaders{
		ID:        h.Get(HeaderWebhookID),
		Timestamp: h.Get(HeaderWebhookTimestamp),
		Signature: h.Get(HeaderWebhookSignature),
	}
}

type VerifyOptions struct {
	Tolerance time.Duration

	Now func() time.Time
}

func VerifyWebhook(secret string, headers WebhookHeaders, rawBody []byte, opts VerifyOptions) error {
	if secret == "" {
		return ErrWebhookSecretMissing
	}
	if headers.ID == "" || headers.Timestamp == "" || headers.Signature == "" {
		return ErrMissingWebhookHeaders
	}

	tolerance := opts.Tolerance
	if tolerance == 0 {
		tolerance = DefaultWebhookTolerance
	}
	if tolerance > 0 {
		ts, err := strconv.ParseInt(headers.Timestamp, 10, 64)
		if err != nil {

			t, terr := time.Parse(time.RFC3339, headers.Timestamp)
			if terr != nil {
				return fmt.Errorf("composio: invalid webhook-timestamp %q: %w", headers.Timestamp, err)
			}
			ts = t.Unix()
		}
		now := time.Now().UTC()
		if opts.Now != nil {
			now = opts.Now().UTC()
		}
		delta := now.Sub(time.Unix(ts, 0))
		if delta < 0 {
			delta = -delta
		}
		if delta > tolerance {
			return fmt.Errorf("%w: drift=%s tolerance=%s", ErrWebhookTimestampStale, delta, tolerance)
		}
	}

	signingString := headers.ID + "." + headers.Timestamp + "." + string(rawBody)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingString))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	candidates := strings.Fields(strings.ReplaceAll(headers.Signature, ",", " "))
	if len(candidates) == 0 {
		return ErrInvalidWebhookSignature
	}
	want := []byte(expected)
	for _, cand := range candidates {

		if len(cand) <= 3 && strings.HasPrefix(cand, "v") {
			continue
		}
		if hmac.Equal([]byte(cand), want) {
			return nil
		}
	}
	return ErrInvalidWebhookSignature
}

func VerifyHTTPRequest(secret string, r *http.Request, opts VerifyOptions) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, errors.New("composio: VerifyHTTPRequest: request body is nil")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("composio: read webhook body: %w", err)
	}
	_ = r.Body.Close()
	if verr := VerifyWebhook(secret, HeadersFromHTTP(r.Header), body, opts); verr != nil {
		return body, verr
	}
	return body, nil
}

type EventEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

func ParseEvent(rawBody []byte) (*EventEnvelope, error) {
	var out EventEnvelope
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("composio: parse webhook envelope: %w", err)
	}
	return &out, nil
}
