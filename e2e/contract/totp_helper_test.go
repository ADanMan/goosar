package contract

// computeTOTP is a client-side implementation of RFC 6238 (HOTP/RFC 4226 on
// SHA1, 6 digits, 30s period) — exactly the parameters
// docs/50-api-contract.yaml's MFAEnrollResponse documents
// (algorithm=SHA1, digits=6, period_seconds=30). It lets the contract suite
// drive a real MFA enroll -> confirm -> login-with-MFA -> verify round trip
// without a human authenticator app.

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // fixed by the contract (MFAEnrollResponse.algorithm = "SHA1")
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// computeTOTP returns the code for the current time step.
func computeTOTP(secretBase32 string) string {
	return computeTOTPAtOffset(secretBase32, 0)
}

// computeTOTPAtOffset returns the code `stepOffset` steps away from the
// current one — the server's anti-replay guard (T-029 доводка:
// Store.ConsumeTOTPStep) rejects a step already accepted, so a caller that
// needs to present a *second*, still-valid code within the same ~30s window
// (e.g. confirm immediately followed by a login-verify) must ask for a
// different step, not recompute the same one.
func computeTOTPAtOffset(secretBase32 string, stepOffset int64) string {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32)
	if err != nil {
		return ""
	}
	counter := uint64(int64(time.Now().Unix())/30 + stepOffset)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}
