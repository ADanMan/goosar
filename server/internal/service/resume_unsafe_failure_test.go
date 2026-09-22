package service

import "testing"

func TestResumeUnsafeFailure(t *testing.T) {
	cases := []struct {
		name          string
		failureReason string
		errorText     string
		want          bool
	}{
		{
			name:          "classified reason wins regardless of text",
			failureReason: "api_invalid_request",
			want:          true,
		},
		{
			name:      "anthropic 400 invalid_request_error text",
			errorText: `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}`,
			want:      true,
		},
		{

			name:      "hermes provider 400 text",
			errorText: `hermes session/prompt failed: session/prompt: Internal error (code=-32603, data=BadRequestError)`,
			want:      true,
		},
		{

			name:      "hermes context window exceeded is resume-safe here",
			errorText: `hermes session/prompt failed: session/prompt: Internal error (code=-32603, data=ContextWindowExceededError)`,
			want:      false,
		},
		{
			name:      "unrelated failure text",
			errorText: "daemon restarted while task was in flight",
			want:      false,
		},
		{
			name: "empty inputs",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResumeUnsafeFailure(tc.failureReason, tc.errorText); got != tc.want {
				t.Errorf("ResumeUnsafeFailure(%q, %q) = %v, want %v", tc.failureReason, tc.errorText, got, tc.want)
			}
		})
	}
}
