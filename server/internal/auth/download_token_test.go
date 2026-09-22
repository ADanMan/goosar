package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testTicketAttachmentID = "0192f3a1-4f2c-7a11-9c3d-8b6f0d2e1a55"
	testTicketUserID       = "0192f3a1-4f2c-7a11-9c3d-000000000001"
)

func mustSignTestTicket(t *testing.T, attachmentID, userID string, expiresAt time.Time) string {
	t.Helper()
	ticket, err := SignAttachmentDownloadTicket(attachmentID, userID, expiresAt)
	if err != nil {
		t.Fatalf("SignAttachmentDownloadTicket: %v", err)
	}
	return ticket
}

func TestAttachmentDownloadTicket_RoundTrip(t *testing.T) {
	expiresAt := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	ticket := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, expiresAt)

	got, err := VerifyAttachmentDownloadTicket(ticket, time.Now())
	if err != nil {
		t.Fatalf("VerifyAttachmentDownloadTicket: %v", err)
	}
	if got.AttachmentID != testTicketAttachmentID {
		t.Fatalf("AttachmentID = %q, want %q", got.AttachmentID, testTicketAttachmentID)
	}
	if got.UserID != testTicketUserID {
		t.Fatalf("UserID = %q, want %q", got.UserID, testTicketUserID)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %s, want %s", got.ExpiresAt, expiresAt)
	}
}

func TestAttachmentDownloadTicket_IsURLSafe(t *testing.T) {
	ticket := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, time.Now().Add(time.Hour))

	for _, c := range ticket {
		isURLSafe := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.'
		if !isURLSafe {
			t.Fatalf("ticket contains character %q that needs URL escaping: %q", c, ticket)
		}
	}
}

func TestAttachmentDownloadTicket_ExpiryIsEnforced(t *testing.T) {
	expiresAt := time.Now().Add(time.Minute).Truncate(time.Second)
	ticket := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, expiresAt)

	if _, err := VerifyAttachmentDownloadTicket(ticket, expiresAt); err != nil {
		t.Fatalf("ticket rejected exactly at exp: %v", err)
	}
	if _, err := VerifyAttachmentDownloadTicket(ticket, expiresAt.Add(time.Second)); !errors.Is(err, ErrDownloadTicketExpired) {
		t.Fatalf("error = %v, want ErrDownloadTicketExpired", err)
	}
}

func TestAttachmentDownloadTicket_RejectsTamperedPayload(t *testing.T) {
	ticket := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, time.Now().Add(time.Hour))
	_, sig, _ := strings.Cut(ticket, ".")

	forged, err := json.Marshal(downloadTicketClaims{
		AttachmentID: "0192f3a1-4f2c-7a11-9c3d-8b6f0d2e1a99",
		UserID:       testTicketUserID,
		Exp:          time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal forged claims: %v", err)
	}
	tampered := base64.RawURLEncoding.EncodeToString(forged) + "." + sig

	if _, err := VerifyAttachmentDownloadTicket(tampered, time.Now()); !errors.Is(err, ErrDownloadTicketSignature) {
		t.Fatalf("error = %v, want ErrDownloadTicketSignature", err)
	}
}

func TestAttachmentDownloadTicket_RejectsMalformed(t *testing.T) {
	valid := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, time.Now().Add(time.Hour))
	payload, sig, _ := strings.Cut(valid, ".")

	unparsablePayload := "!!!not-base64!!!"
	signedGarbage := unparsablePayload + "." + signDownloadTicketPayload(unparsablePayload)

	for name, ticket := range map[string]string{
		"empty":            "",
		"no separator":     payload + sig,
		"empty payload":    "." + sig,
		"empty signature":  payload + ".",
		"signed non-b64":   signedGarbage,
		"signed non-json":  signSyntheticPayload(t, []byte("not json")),
		"empty attachment": signSyntheticClaims(t, downloadTicketClaims{UserID: testTicketUserID, Exp: time.Now().Add(time.Hour).Unix()}),
		"empty user":       signSyntheticClaims(t, downloadTicketClaims{AttachmentID: testTicketAttachmentID, Exp: time.Now().Add(time.Hour).Unix()}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyAttachmentDownloadTicket(ticket, time.Now()); !errors.Is(err, ErrDownloadTicketMalformed) {
				t.Fatalf("error = %v, want ErrDownloadTicketMalformed", err)
			}
		})
	}
}

func TestAttachmentDownloadTicket_IsDomainSeparatedFromTheRawSecret(t *testing.T) {
	ticket := mustSignTestTicket(t, testTicketAttachmentID, testTicketUserID, time.Now().Add(time.Hour))
	payload, sig, _ := strings.Cut(ticket, ".")

	mac := hmac.New(sha256.New, JWTSecret())
	mac.Write([]byte(payload))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) == sig {
		t.Fatal("ticket is signed with the raw JWT secret; the signing key must be domain-separated so a ticket can never be confused with another credential")
	}

	if _, err := VerifyAttachmentDownloadTicket("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1In0.sig", time.Now()); err == nil {
		t.Fatal("a JWT-shaped string verified as a download ticket")
	}
}

func signSyntheticPayload(t *testing.T, raw []byte) string {
	t.Helper()
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + signDownloadTicketPayload(payload)
}

func signSyntheticClaims(t *testing.T, claims downloadTicketClaims) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return signSyntheticPayload(t, raw)
}
