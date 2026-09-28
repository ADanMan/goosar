package importer

import (
	"context"
)

// writeAll пишет весь снимок f в server2 в порядке зависимостей миграций
// (001…010, см. docs/51-data-model.md «Порядок вызовов»): accounts до всего,
// что на них ссылается; spaces до всего workspace-scoped; executors до
// operatives; capabilities до operative_capabilities; initiatives до tickets;
// tickets (без tk_parent_ticket_id) до второго прохода по родителям и до
// ticket_notes/marks/subscribers/pr_links; autopilots/convos последними,
// т.к. ссылаются на initiatives/operatives/tickets.
func writeAll(ctx context.Context, w *writer, r *Report, f *fetched) error {
	// accounts + space_members — учётные записи должны существовать раньше
	// всего, что на них ссылается (created_by/owner_account_id/creator_id и т.д.)
	for _, m := range f.members {
		if err := w.upsertAccount(ctx, m.UserID, m.Email, m.Name, m.AvatarURL); err != nil {
			return err
		}
	}
	if f.me != nil {
		if err := w.upsertAccount(ctx, f.me.ID, f.me.Email, f.me.Name, f.me.AvatarURL); err != nil {
			return err
		}
	}

	if err := w.upsertSpace(ctx, f.workspace); err != nil {
		return err
	}

	for _, m := range f.members {
		if err := w.upsertSpaceMember(ctx, f.workspace.ID, m); err != nil {
			return err
		}
	}

	for _, p := range f.protocols {
		if err := w.upsertRuntimeProfile(ctx, p); err != nil {
			return err
		}
	}

	runtimeIDs := map[string]bool{}
	for _, rt := range f.runtimes {
		if err := w.upsertExecutor(ctx, rt); err != nil {
			return err
		}
		runtimeIDs[rt.ID] = true
	}

	for _, s := range f.skills {
		if err := w.upsertCapability(ctx, s); err != nil {
			return err
		}
		for _, file := range s.Files {
			if err := w.upsertCapabilityFile(ctx, file); err != nil {
				return err
			}
		}
	}

	for _, a := range f.agents {
		if !runtimeIDs[a.RuntimeID] {
			if err := w.ensureExecutorPlaceholder(ctx, a.RuntimeID, f.workspace.ID); err != nil {
				return err
			}
			runtimeIDs[a.RuntimeID] = true
		}
		if err := w.upsertOperative(ctx, a.RuntimeID, a); err != nil {
			return err
		}
		for _, t := range a.InvocationTargets {
			if err := w.upsertOperativeTarget(ctx, a.ID, t); err != nil {
				return err
			}
		}
		for _, sk := range a.Skills {
			if err := w.upsertOperativeCapability(ctx, a.ID, sk); err != nil {
				return err
			}
		}
	}

	for _, s := range f.squads {
		if err := w.upsertCrew(ctx, s); err != nil {
			return err
		}
		for _, m := range f.squadMembers[s.ID] {
			if err := w.upsertCrewMember(ctx, s.ID, m); err != nil {
				return err
			}
		}
	}

	for _, rt := range []string{"issue", "agent", "skill"} {
		for _, l := range f.labelsByResource[rt] {
			if err := w.upsertTag(ctx, l); err != nil {
				return err
			}
		}
	}

	for _, p := range f.properties {
		if err := w.upsertFieldDef(ctx, p); err != nil {
			return err
		}
	}

	for _, p := range f.projects {
		if err := w.upsertInitiative(ctx, p); err != nil {
			return err
		}
		for _, res := range f.projectResources[p.ID] {
			if err := w.upsertInitiativeResource(ctx, f.workspace.ID, res); err != nil {
				return err
			}
		}
	}

	maxSeq := 0
	labelByID := indexLabelsByID(f.labelsByResource["issue"])
	for _, i := range f.issues {
		if err := w.upsertTicket(ctx, i); err != nil {
			return err
		}
		if i.Number > maxSeq {
			maxSeq = i.Number
		}
		for _, lbl := range i.Labels {
			if _, ok := labelByID[lbl.ID]; ok {
				if err := w.linkTicketTag(ctx, i.ID, lbl.ID); err != nil {
					return err
				}
			}
		}
		for _, reaction := range i.Reactions {
			if err := w.upsertTicketMark(ctx, i.ID, reaction); err != nil {
				return err
			}
		}
	}
	// Второй проход: родитель-задача теперь гарантированно существует (или
	// не существует вовсе — тогда tk_parent_ticket_id остаётся NULL, а не
	// валит перенос).
	issueByID := map[string]sourceIssue{}
	for _, i := range f.issues {
		issueByID[i.ID] = i
	}
	for _, i := range f.issues {
		if i.ParentIssueID == nil {
			continue
		}
		if _, ok := issueByID[*i.ParentIssueID]; !ok {
			continue
		}
		if err := w.setTicketParent(ctx, i.ID, *i.ParentIssueID); err != nil {
			return err
		}
	}
	if maxSeq > 0 {
		if err := w.bumpTicketSeq(ctx, f.workspace.ID, maxSeq); err != nil {
			return err
		}
	}

	for _, i := range f.issues {
		comments := f.comments[i.ID]
		for _, c := range comments {
			if err := w.upsertTicketNote(ctx, c); err != nil {
				return err
			}
			for _, reaction := range c.Reactions {
				if err := w.upsertNoteMark(ctx, c.ID, reaction); err != nil {
					return err
				}
			}
		}
		commentByID := map[string]sourceIssueComment{}
		for _, c := range comments {
			commentByID[c.ID] = c
		}
		for _, c := range comments {
			if c.ParentID == nil {
				continue
			}
			if _, ok := commentByID[*c.ParentID]; !ok {
				continue
			}
			if err := w.setTicketNoteParent(ctx, c.ID, *c.ParentID); err != nil {
				return err
			}
		}

		for _, s := range f.subscribers[i.ID] {
			if err := w.upsertTicketSubscriber(ctx, s); err != nil {
				return err
			}
		}
		for _, pr := range f.prLinks[i.ID] {
			if err := w.upsertTicketPRLink(ctx, pr); err != nil {
				return err
			}
		}
	}

	for _, a := range f.autopilots {
		if err := w.upsertSentinel(ctx, a); err != nil {
			return err
		}
		for _, t := range f.autopilotTriggers[a.ID] {
			if err := w.upsertSentinelTrigger(ctx, t); err != nil {
				return err
			}
		}
	}

	for _, s := range f.chatSessions {
		if err := w.upsertConvo(ctx, s); err != nil {
			return err
		}
		for _, m := range f.chatMessages[s.ID] {
			if err := w.upsertConvoMessage(ctx, s.ID, m); err != nil {
				return err
			}
		}
	}

	for _, m := range f.mcpServers {
		if err := w.upsertWorkspaceMcpServer(ctx, f.workspace.ID, m); err != nil {
			return err
		}
	}
	if f.config != nil {
		if err := w.upsertSpaceConfig(ctx, f.workspace.ID, *f.config); err != nil {
			return err
		}
	}

	countFetched(r, f)
	return nil
}

func indexLabelsByID(labels []sourceLabel) map[string]sourceLabel {
	out := make(map[string]sourceLabel, len(labels))
	for _, l := range labels {
		out[l.ID] = l
	}
	return out
}
