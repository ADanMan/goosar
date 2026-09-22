package service

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDevVerificationCodeGoesToStderrNotStdout(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("GOOSAR_DELIVERY_PROFILE", "")
	t.Setenv("FRONTEND_ORIGIN", "")

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = origOut, origErr })

	svc := &EmailService{}
	if err := svc.SendVerificationCode("dev@example.com", "424242", "", "en"); err != nil {
		t.Fatalf("SendVerificationCode: %v", err)
	}

	outW.Close()
	errW.Close()
	stdout, _ := io.ReadAll(outR)
	stderr, _ := io.ReadAll(errR)

	if strings.Contains(string(stdout), "424242") {
		t.Fatalf("the code reached the audit stdout stream: %q", stdout)
	}
	if !strings.Contains(string(stderr), "424242") {
		t.Fatalf("the dev code must still be visible on stderr, got %q", stderr)
	}
}
