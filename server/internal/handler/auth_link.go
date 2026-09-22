package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/audit"
)

const loginLinkTokenBytes = 32

const maxLoginLinkTokenLen = 512

func generateLoginLinkToken() (string, error) {
	buf := make([]byte, loginLinkTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashLoginLinkToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type VerifyLinkRequest struct {
	LinkToken string `json:"link_token"`
}

func (h *Handler) VerifyLink(w http.ResponseWriter, r *http.Request) {
	var req VerifyLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token := strings.TrimSpace(req.LinkToken)
	if token == "" || len(token) > maxLoginLinkTokenLen {
		writeError(w, http.StatusBadRequest, "invalid or expired link")
		return
	}

	hash := pgtype.Text{String: hashLoginLinkToken(token), Valid: true}
	dbCode, err := h.Queries.GetVerificationCodeByLinkTokenHash(r.Context(), hash)
	if err != nil {

		h.auditSignIn(r, audit.ActionLoginLinkFailed, "", audit.OutcomeFailure, audit.ReasonInvalidLink)
		writeError(w, http.StatusBadRequest, "invalid or expired link")
		return
	}

	rows, err := h.Queries.ConsumeVerificationCode(r.Context(), dbCode.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify link")
		return
	}
	if rows == 0 {

		h.auditSignIn(r, audit.ActionLoginLinkFailed, dbCode.Email, audit.OutcomeFailure, audit.ReasonCodeAlreadyUsed)
		writeError(w, http.StatusBadRequest, "invalid or expired link")
		return
	}

	h.completeLogin(w, r, dbCode.Email, audit.ActionLoginLinkVerified)
}
