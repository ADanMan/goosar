package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func mfaTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRotateAcceptsTheMFADomain(t *testing.T) {
	opts, err := parseRotateArgs([]string{"--mfa"})
	if err != nil || !opts.mfa || opts.mcp {
		t.Fatalf("--mfa: opts=%+v err=%v", opts, err)
	}
	opts, err = parseRotateArgs([]string{"--mcp", "--mfa", "--dry-run"})
	if err != nil || !opts.mfa || !opts.mcp || !opts.dryRun {
		t.Fatalf("--mcp --mfa --dry-run: opts=%+v err=%v", opts, err)
	}
	if _, err := parseRotateArgs(nil); err == nil {
		t.Fatal("no domain flag was accepted")
	}
	if got := rotateDomainLabel(rotateOptions{mfa: true}); got != "mfa" {
		t.Fatalf("audit label: %q", got)
	}
}

func TestRotateMFASecretsResealsUnderTheCurrentKey(t *testing.T) {
	pool := mfaTestPool(t)
	if pool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	queries := db.New(pool)

	oldKey := bytes.Repeat([]byte{0x11}, secretbox.KeySize)
	newKey := bytes.Repeat([]byte{0x22}, secretbox.KeySize)
	oldBox, err := secretbox.New(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := secretbox.NewRing(newKey, [][]byte{oldKey})
	if err != nil {
		t.Fatal(err)
	}

	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Name: "rotate probe", Email: "mfa-rotate-probe@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = queries.DeleteUserMFA(bg, user.ID)
		_, _ = pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, uuidText(user.ID))
	})

	secret := []byte("12345678901234567890")
	sealed, err := oldBox.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.UpsertUserMFASecret(ctx, db.UpsertUserMFASecretParams{
		UserID: user.ID, TotpSecretSealed: sealed,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out bytes.Buffer
	stats, err := rotateMFASecrets(ctx, queries, ring, false, &out)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if stats.resealed != 1 {
		t.Fatalf("resealed %d, want 1 (%s)", stats.resealed, out.String())
	}
	row, err := queries.GetUserMFA(ctx, user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !ring.SealedWithCurrentKey(row.TotpSecretSealed) {
		t.Fatal("the row is still on the retired key")
	}
	plain, err := ring.Open(row.TotpSecretSealed)
	if err != nil || string(plain) != string(secret) {
		t.Fatalf("the secret did not survive the reseal: %q (err=%v)", plain, err)
	}

	out.Reset()
	stats, err = rotateMFASecrets(ctx, queries, ring, false, &out)
	if err != nil || stats.resealed != 0 || stats.skipped != 1 {
		t.Fatalf("second pass: %+v err=%v", stats, err)
	}
}

func TestMFAResetRemovesEverythingAndCutsSessions(t *testing.T) {
	pool := mfaTestPool(t)
	if pool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	queries := db.New(pool)

	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Name: "reset probe", Email: "mfa-reset-probe@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = queries.DeleteUserMFA(bg, user.ID)
		_ = queries.DeleteUserMFARecoveryCodes(bg, user.ID)
		_ = queries.DeleteUserSessions(bg, user.ID)
		_, _ = pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, uuidText(user.ID))
	})

	if _, err := queries.UpsertUserMFASecret(ctx, db.UpsertUserMFASecretParams{
		UserID: user.ID, TotpSecretSealed: []byte("sealed-enough-for-a-test"),
	}); err != nil {
		t.Fatalf("seed factor: %v", err)
	}
	if _, err := queries.ConfirmUserMFA(ctx, db.ConfirmUserMFAParams{UserID: user.ID, LastUsedStep: 1}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := queries.InsertUserMFARecoveryCode(ctx, db.InsertUserMFARecoveryCodeParams{
		UserID: user.ID, CodeHash: "deadbeef",
	}); err != nil {
		t.Fatalf("seed code: %v", err)
	}
	if _, err := queries.CreateUserSession(ctx, db.CreateUserSessionParams{UserID: user.ID}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var out bytes.Buffer
	if err := resetMFA(ctx, queries, &out, []string{"MFA-Reset-Probe@Example.com"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("output does not say what happened: %s", out.String())
	}
	if _, err := queries.GetUserMFA(ctx, user.ID); err == nil {
		t.Fatal("the factor survived the reset")
	}
	if left, _ := queries.CountUserMFARecoveryCodesUnused(ctx, user.ID); left != 0 {
		t.Fatalf("%d recovery codes survived the reset", left)
	}
	live, _ := queries.ListUserSessions(ctx, user.ID)
	if len(live) != 0 {
		t.Fatalf("%d sessions survived the reset", len(live))
	}
	after, err := queries.GetUser(ctx, user.ID)
	if err != nil || after.TokenVersion <= user.TokenVersion {
		t.Fatalf("the session epoch did not move: %d -> %d", user.TokenVersion, after.TokenVersion)
	}
}

func TestMFAResetRejectsAnUnknownAddress(t *testing.T) {
	pool := mfaTestPool(t)
	if pool == nil {
		t.Skip("no test database")
	}
	var out bytes.Buffer
	err := resetMFA(context.Background(), db.New(pool), &out, []string{"nobody-391@example.com"})
	if err == nil || !strings.Contains(err.Error(), "no account") {
		t.Fatalf("unknown address: %v", err)
	}
	if err := resetMFA(context.Background(), db.New(pool), &out, nil); err == nil {
		t.Fatal("mfa-reset accepted no argument")
	}
}
