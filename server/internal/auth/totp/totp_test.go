package totp

import (
	"strings"
	"testing"
	"time"
)

var rfcSecret = []byte("12345678901234567890")

func TestCodeMatchesRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, tc := range cases {
		got := Code(rfcSecret, Step64(time.Unix(tc.unix, 0)))
		if got != tc.want {
			t.Errorf("Code at unix %d = %q, want %q", tc.unix, got, tc.want)
		}
	}
}

func TestValidateAcceptsCurrentAndAdjacentSteps(t *testing.T) {
	at := time.Unix(1111111109, 0)
	now := Step64(at)
	for _, delta := range []int64{-1, 0, 1} {
		step, ok := Validate(rfcSecret, Code(rfcSecret, now+delta), at)
		if !ok {
			t.Fatalf("delta %d: code rejected", delta)
		}
		if step != now+delta {
			t.Errorf("delta %d: matched step %d, want %d", delta, step, now+delta)
		}
	}
}

func TestValidateRejectsOutOfWindowAndMalformedCodes(t *testing.T) {
	at := time.Unix(1111111109, 0)
	now := Step64(at)
	for _, code := range []string{
		Code(rfcSecret, now+2),
		Code(rfcSecret, now-2),
		"000000",
		"12345",
		"1234567",
		"",
		"abcdef",
	} {
		if _, ok := Validate(rfcSecret, code, at); ok {
			t.Errorf("code %q accepted, want rejected", code)
		}
	}
}

func TestSecretRoundTripsThroughManualEntryForms(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != SecretBytes {
		t.Fatalf("secret is %d bytes, want %d", len(secret), SecretBytes)
	}
	encoded := EncodeSecret(secret)

	for _, typed := range []string{
		encoded,
		strings.ToLower(encoded),
		encoded[:4] + " " + encoded[4:],
		encoded + "====",
	} {
		got, err := DecodeSecret(typed)
		if err != nil {
			t.Fatalf("DecodeSecret(%q): %v", typed, err)
		}
		if string(got) != string(secret) {
			t.Errorf("DecodeSecret(%q) did not round-trip", typed)
		}
	}
}

func TestURICarriesTheParametersAuthenticatorsRead(t *testing.T) {
	uri := URI("Goosar", "user@example.com", rfcSecret)
	for _, want := range []string{
		"otpauth://totp/",
		"secret=" + EncodeSecret(rfcSecret),
		"issuer=Goosar",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI %q missing %q", uri, want)
		}
	}
}
