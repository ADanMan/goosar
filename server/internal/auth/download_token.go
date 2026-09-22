package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrDownloadTicketMalformed = errors.New("auth: download ticket malformed")

	ErrDownloadTicketSignature = errors.New("auth: download ticket signature mismatch")

	ErrDownloadTicketExpired = errors.New("auth: download ticket expired")
)

const downloadTicketKeyContext = "goosar/attachment-download-ticket/v1"

type downloadTicketClaims struct {
	AttachmentID string `json:"a"`

	UserID string `json:"u"`
	Exp    int64  `json:"e"`
}

func (c downloadTicketClaims) valid() bool {
	return c.AttachmentID != "" && c.UserID != ""
}

type AttachmentDownloadTicket struct {
	AttachmentID string
	UserID       string
	ExpiresAt    time.Time
}

func SignAttachmentDownloadTicket(attachmentID, userID string, expiresAt time.Time) (string, error) {
	claims := downloadTicketClaims{
		AttachmentID: attachmentID,
		UserID:       userID,
		Exp:          expiresAt.Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + signDownloadTicketPayload(payload), nil
}

func VerifyAttachmentDownloadTicket(ticket string, now time.Time) (AttachmentDownloadTicket, error) {
	payload, sig, hasSep := strings.Cut(ticket, ".")
	if !hasSep || payload == "" || sig == "" {
		return AttachmentDownloadTicket{}, ErrDownloadTicketMalformed
	}
	if !hmac.Equal([]byte(sig), []byte(signDownloadTicketPayload(payload))) {
		return AttachmentDownloadTicket{}, ErrDownloadTicketSignature
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return AttachmentDownloadTicket{}, ErrDownloadTicketMalformed
	}

	var claims downloadTicketClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return AttachmentDownloadTicket{}, ErrDownloadTicketMalformed
	}

	if !claims.valid() {
		return AttachmentDownloadTicket{}, ErrDownloadTicketMalformed
	}
	if claims.Exp < now.Unix() {
		return AttachmentDownloadTicket{}, ErrDownloadTicketExpired
	}

	return AttachmentDownloadTicket{
		AttachmentID: claims.AttachmentID,
		UserID:       claims.UserID,
		ExpiresAt:    time.Unix(claims.Exp, 0),
	}, nil
}

func downloadTicketKey() []byte {
	mac := hmac.New(sha256.New, JWTSecret())
	mac.Write([]byte(downloadTicketKeyContext))
	return mac.Sum(nil)
}

func signDownloadTicketPayload(payload string) string {
	mac := hmac.New(sha256.New, downloadTicketKey())
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
