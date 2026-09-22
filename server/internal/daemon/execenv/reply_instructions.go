package execenv

import "fmt"

func BuildNewCommentsHint(issueID, triggerCommentID, triggerThreadID, newCommentsSince string, newCommentCount int) string {
	if newCommentCount <= 0 || newCommentsSince == "" || issueID == "" {
		return ""
	}
	threadID := activeThreadID(triggerThreadID, triggerCommentID)

	if threadID != "" {
		return fmt.Sprintf(
			"%d new comment(s) on this issue since your last run — don't read them all blindly. "+
				"Start with the thread your triggering comment is in: "+
				"`goosar issue comment list %s --thread %s --since %s --output json` "+
				"(swap `--since` for `--tail 30` if you need the full thread, not just the delta). "+
				"Only if you need context from the other threads, catch up issue-wide: "+
				"`goosar issue comment list %s --since %s --output json`.\n\n",
			newCommentCount, issueID, threadID, newCommentsSince, issueID, newCommentsSince,
		)
	}

	return fmt.Sprintf(
		"%d new comment(s) on this issue since your last run. Catch up: "+
			"`goosar issue comment list %s --since %s --output json`.\n\n",
		newCommentCount, issueID, newCommentsSince,
	)
}

func BuildResumedCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	return fmt.Sprintf(
		"You're resuming the prior session, and the triggering comment is already included above. "+
			"No other new comments on this issue since your last run. "+
			"Use the active thread anchor `%s` and triggering comment ID `%s`. "+
			"If your reply depends on thread context, do not rely only on resumed session memory — "+
			"first pull the triggering conversation with: "+
			"`goosar issue comment list %s --thread %s --tail 30 --output json`.\n\n",
		threadID, triggerCommentID, issueID, threadID,
	)
}

func BuildColdCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	return fmt.Sprintf(
		"Read the triggering conversation first: "+
			"`goosar issue comment list %s --thread %s --tail 30 --output json` "+
			"(that thread's root + its 30 newest replies). "+
			"Need cross-thread background? `goosar issue comment list %s --recent 10 --output json` "+
			"(resolved threads come back folded — `--full` to expand).\n\n",
		issueID, threadID, issueID,
	)
}

func activeThreadID(triggerThreadID, triggerCommentID string) string {
	if triggerThreadID != "" {
		return triggerThreadID
	}
	return triggerCommentID
}

func BuildCommentReplyInstructions(provider, issueID, triggerCommentID string) string {
	if triggerCommentID == "" {
		return ""
	}
	return buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID)
}

func buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID string) string {
	if runtimeGOOS == "windows" {
		return fmt.Sprintf(
			"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
				"do NOT reuse --parent values from previous turns in this session.\n\n"+
				"On Windows, write the reply body to a UTF-8 file with your file-write tool first, then post with `--content-file`. "+
				"Do NOT pipe via `--content-stdin` — PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping to native commands and silently drops non-ASCII (Chinese, Japanese, Cyrillic, accents, emoji) as `?` before bytes reach `goosar.exe`. "+
				"See ## Comment Formatting above for the full rule:\n\n"+
				"    goosar issue comment add %s --parent %s --content-file ./reply.md\n"+
				"    Remove-Item ./reply.md\n\n"+
				"Do NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
			issueID, triggerCommentID,
		)
	}
	return fmt.Sprintf(
		"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session.\n\n"+
			"Write the reply body to a UTF-8 file with your file-write tool first, then post it with `--content-file` "+
			"(see ## Comment Formatting above for why inline `--content` and `--content-stdin` HEREDOCs are unsafe — MUL-2904 / #4182):\n\n"+
			"    goosar issue comment add %s --parent %s --content-file ./reply.md\n"+
			"    rm ./reply.md\n\n"+
			"Do NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
		issueID, triggerCommentID,
	)
}

type ThreadReplyTarget struct {
	ThreadID string
	ParentID string
}

func BuildMultiThreadCommentReplyInstructions(issueID string, targets []ThreadReplyTarget) string {
	if issueID == "" || len(targets) < 2 {
		return ""
	}

	targetLines := ""
	for i, tgt := range targets {
		targetLines += fmt.Sprintf("%d. thread %s → reply with `--parent %s`\n", i+1, tgt.ThreadID, tgt.ParentID)
	}

	var cookbook string
	if runtimeGOOS == "windows" {
		cookbook = fmt.Sprintf(
			"For EACH thread above, write that reply's body to its own UTF-8 file with your file-write tool, then post it with `--content-file` (do NOT use inline `--content` or a `--content-stdin` HEREDOC — see ## Comment Formatting above for why). Use a DISTINCT file per thread (never reuse one file) and remove each after posting:\n\n"+
				"    goosar issue comment add %s --parent <thread-1-parent> --content-file ./reply-1.md\n"+
				"    Remove-Item ./reply-1.md\n"+
				"    goosar issue comment add %s --parent <thread-2-parent> --content-file ./reply-2.md\n"+
				"    Remove-Item ./reply-2.md\n\n",
			issueID, issueID,
		)
	} else {
		cookbook = fmt.Sprintf(
			"For EACH thread above, write that reply's body to its own UTF-8 file with your file-write tool, then post it with `--content-file` (do NOT use inline `--content` or a `--content-stdin` HEREDOC — see ## Comment Formatting above for why). Use a DISTINCT file per thread (never reuse one file) and remove each after posting:\n\n"+
				"    goosar issue comment add %s --parent <thread-1-parent> --content-file ./reply-1.md\n"+
				"    rm ./reply-1.md\n"+
				"    goosar issue comment add %s --parent <thread-2-parent> --content-file ./reply-2.md\n"+
				"    rm ./reply-2.md\n\n",
			issueID, issueID,
		)
	}

	return fmt.Sprintf(
		"This run coalesced comments from %d DISTINCT threads. Post ONE reply per thread — %d replies in total — each threaded under its own conversation. This OVERRIDES the general \"post exactly one comment per run\" guidance: for THIS run multiple replies are required and correct. Do NOT merge separate threads into a single comment, and do NOT post more than one reply in the same thread.\n\n"+
			"Post the replies in the order listed below — OLDEST thread first, the newest (triggering) thread LAST — so they land in chronological order. Do NOT answer the newest/triggering comment first.\n\n"+
			"Reply targets, in the order to post them (use the exact `--parent` for each — do NOT reuse `--parent` values from previous turns in this session):\n"+
			"%s\n"+
			"%s"+
			"Do NOT write literal `\\n` escapes to simulate line breaks; each file preserves real newlines.\n",
		len(targets), len(targets), targetLines, cookbook,
	)
}
