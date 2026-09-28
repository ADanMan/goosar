package authn

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPRoundTripAndClockSkew(t *testing.T) {
	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("newTOTPSecret: %v", err)
	}
	now := totpCounter(time.Now())
	current, prev, next, far := hotp(secret, now), hotp(secret, now-1), hotp(secret, now+1), hotp(secret, now-5)

	for _, tc := range []struct {
		name string
		code string
		want bool
	}{
		{"current step", current, true},
		{"one step back (clock skew)", prev, true},
		{"one step forward (clock skew)", next, true},
		{"five steps back, outside tolerance", far, false},
		{"garbage code", "000000", false},
	} {
		if got := validateTOTP(secret, tc.code); got != tc.want {
			t.Errorf("%s: validateTOTP = %v, want %v", tc.name, got, tc.want)
		}
	}
	if len(current) != totpDigits {
		t.Fatalf("code length = %d, want %d", len(current), totpDigits)
	}
}

func TestOtpauthURIContainsExpectedFields(t *testing.T) {
	secret, _ := newTOTPSecret()
	uri := otpauthURI("Goosar", "user@example.test", secret)
	for _, want := range []string{"otpauth://totp/", "issuer=Goosar", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Errorf("otpauthURI missing %q, got %q", want, uri)
		}
	}
}
