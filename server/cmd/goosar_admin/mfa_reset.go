package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const mfaResetUsage = `goosar_admin mfa-reset <email>

Removes the TOTP second factor and every recovery code of one account, and
revokes its sessions. The person signs in with their first factor alone and
enrolls a new authenticator.

Journaled in admin_audit as mfa.reset.`

func resetMFA(ctx context.Context, queries *db.Queries, out io.Writer, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return errors.New("mfa-reset requires exactly one <email> argument\n\n" + mfaResetUsage)
	}
	email := strings.ToLower(strings.TrimSpace(args[0]))

	user, err := queries.GetUserByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("no account with email %q", email)
	}

	row, mfaErr := queries.GetUserMFA(ctx, user.ID)
	hadFactor := mfaErr == nil && row.EnabledAt.Valid

	if err := queries.DeleteUserMFA(ctx, user.ID); err != nil {
		return fmt.Errorf("remove the second factor: %w", err)
	}
	if err := queries.DeleteUserMFARecoveryCodes(ctx, user.ID); err != nil {
		return fmt.Errorf("remove the recovery codes: %w", err)
	}

	revoked, err := queries.RevokeAllUserSessions(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	updated, err := queries.BumpUserTokenVersion(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	if _, err := queries.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		Action:     "mfa.reset",
		TargetType: "user",
		TargetID:   pgText(uuidText(user.ID)),

		AfterHash: pgText(fmt.Sprintf("had_factor=%t sessions_revoked=%d token_version=%d",
			hadFactor, revoked, updated.TokenVersion)),
		RequestID: pgText("goosar_admin mfa-reset"),
	}); err != nil {

		fmt.Fprintf(out, "warning: the reset succeeded but the audit row could not be written: %v\n", err)
	}

	if !hadFactor {
		fmt.Fprintf(out, "account %s had no active second factor; sessions revoked anyway (%d)\n",
			uuidText(user.ID), revoked)
		return nil
	}
	fmt.Fprintf(out, "second factor and recovery codes removed for %s; %d session(s) revoked.\n",
		uuidText(user.ID), revoked)
	fmt.Fprintln(out, "The person signs in with their first factor and enrolls a new authenticator in Settings → Security.")
	return nil
}
