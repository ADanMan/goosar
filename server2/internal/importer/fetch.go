package importer

import (
	"context"
	"fmt"
)

// fetched — весь снимок исходного воркспейса, прочитанный через API одной
// пачкой перед записью. Держать всё в памяти оправдано: T-025 переносит один
// воркспейс за раз (не весь деплой), а объём одного воркспейса ограничен
// (contract: listIssues limit=100/страница, до 2000 комментариев на задачу).
type fetched struct {
	workspace sourceWorkspace
	members   []sourceMember
	protocols []sourceRuntimeProfile
	runtimes  []sourceRuntime
	agents    []sourceAgent

	skills []sourceSkillWithFiles

	squads        []sourceSquad
	squadMembers  map[string][]sourceSquadMember // squad id -> members

	labelsByResource map[string][]sourceLabel // "issue"|"agent"|"skill" -> labels
	properties       []sourceProperty

	projects         []sourceProject
	projectResources map[string][]sourceProjectResource // project id -> resources

	issues       []sourceIssue
	comments     map[string][]sourceIssueComment           // issue id -> comments
	subscribers  map[string][]sourceIssueSubscriber        // issue id -> subscribers
	prLinks      map[string][]sourceIssuePullRequestLink   // issue id -> pr links

	autopilots       []sourceAutopilot
	autopilotTriggers map[string][]sourceAutopilotTrigger // autopilot id -> triggers

	chatSessions []sourceChatSession
	chatMessages map[string][]sourceChatMessage // session id -> messages

	mcpServers []sourceWorkspaceMcpServer
	config     *sourceWorkspaceConfigLayer

	// me — вызывающий (владелец PAT), используется как fallback-автор для
	// записей, у которых источник не сообщает автора явно (например
	// cfg_updated_by конфигурации, если WorkspaceConfigLayer.updated_by пуст).
	me *sourceUser
}

func fetchAll(ctx context.Context, api *client, ws *sourceWorkspace) (*fetched, error) {
	f := &fetched{
		workspace:         *ws,
		squadMembers:      map[string][]sourceSquadMember{},
		labelsByResource:  map[string][]sourceLabel{},
		projectResources:  map[string][]sourceProjectResource{},
		comments:          map[string][]sourceIssueComment{},
		subscribers:       map[string][]sourceIssueSubscriber{},
		prLinks:           map[string][]sourceIssuePullRequestLink{},
		autopilotTriggers: map[string][]sourceAutopilotTrigger{},
		chatMessages:      map[string][]sourceChatMessage{},
	}

	var err error
	if f.me, err = api.getMe(ctx); err != nil {
		return nil, fmt.Errorf("importer: /api/me: %w", err)
	}
	if f.members, err = api.listMembers(ctx, ws.ID); err != nil {
		return nil, fmt.Errorf("importer: участники: %w", err)
	}
	if f.protocols, err = api.listRuntimeProfiles(ctx, ws.ID); err != nil {
		return nil, fmt.Errorf("importer: runtime-profiles: %w", err)
	}
	if f.runtimes, err = api.listRuntimes(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: runtimes: %w", err)
	}
	if f.agents, err = api.listAgents(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: agents: %w", err)
	}

	skillSummaries, err := api.listSkills(ctx, ws.Slug)
	if err != nil {
		return nil, fmt.Errorf("importer: skills: %w", err)
	}
	for _, s := range skillSummaries {
		full, err := api.getSkill(ctx, ws.Slug, s.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: skill %s: %w", s.ID, err)
		}
		f.skills = append(f.skills, *full)
	}

	if f.squads, err = api.listSquads(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: squads: %w", err)
	}
	for _, s := range f.squads {
		members, err := api.listSquadMembers(ctx, ws.Slug, s.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: squad %s members: %w", s.ID, err)
		}
		f.squadMembers[s.ID] = members
	}

	for _, rt := range []string{"issue", "agent", "skill"} {
		labels, err := api.listLabels(ctx, ws.Slug, rt)
		if err != nil {
			return nil, fmt.Errorf("importer: labels(%s): %w", rt, err)
		}
		f.labelsByResource[rt] = labels
	}
	if f.properties, err = api.listProperties(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: properties: %w", err)
	}

	if f.projects, err = api.listProjects(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: projects: %w", err)
	}
	for _, p := range f.projects {
		resources, err := api.listProjectResources(ctx, ws.Slug, p.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: project %s resources: %w", p.ID, err)
		}
		f.projectResources[p.ID] = resources
	}

	if f.issues, err = api.listAllIssues(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: issues: %w", err)
	}
	for _, i := range f.issues {
		comments, err := api.listIssueComments(ctx, ws.Slug, i.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: issue %s comments: %w", i.ID, err)
		}
		f.comments[i.ID] = comments

		subs, err := api.listIssueSubscribers(ctx, ws.Slug, i.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: issue %s subscribers: %w", i.ID, err)
		}
		f.subscribers[i.ID] = subs

		prs, err := api.listIssuePullRequests(ctx, ws.Slug, i.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: issue %s pull-requests: %w", i.ID, err)
		}
		f.prLinks[i.ID] = prs
	}

	if f.autopilots, err = api.listAutopilots(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: autopilots: %w", err)
	}
	for _, a := range f.autopilots {
		full, err := api.getAutopilot(ctx, ws.Slug, a.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: autopilot %s: %w", a.ID, err)
		}
		f.autopilotTriggers[a.ID] = full.Triggers
	}

	if f.chatSessions, err = api.listChatSessions(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: chat sessions: %w", err)
	}
	for _, s := range f.chatSessions {
		msgs, err := api.listAllChatMessages(ctx, ws.Slug, s.ID)
		if err != nil {
			return nil, fmt.Errorf("importer: chat session %s messages: %w", s.ID, err)
		}
		f.chatMessages[s.ID] = msgs
	}

	if f.mcpServers, err = api.listWorkspaceMcpServers(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: mcp-servers: %w", err)
	}
	if f.config, err = api.getWorkspaceConfig(ctx, ws.Slug); err != nil {
		return nil, fmt.Errorf("importer: workspace-config: %w", err)
	}

	return f, nil
}

func countFetched(r *Report, f *fetched) {
	r.add("spaces", 1)
	r.add("space_members", len(f.members))
	r.add("agent_protocols", len(f.protocols))
	r.add("executors", len(f.runtimes))
	r.add("operatives", len(f.agents))
	r.add("capabilities", len(f.skills))
	nFiles := 0
	for _, s := range f.skills {
		nFiles += len(s.Files)
	}
	r.add("capability_files", nFiles)
	r.add("crews", len(f.squads))
	nCrewMembers := 0
	for _, m := range f.squadMembers {
		nCrewMembers += len(m)
	}
	r.add("crew_members", nCrewMembers)
	r.add("tags", len(f.labelsByResource["issue"])+len(f.labelsByResource["agent"])+len(f.labelsByResource["skill"]))
	r.add("field_defs", len(f.properties))
	r.add("initiatives", len(f.projects))
	nResources := 0
	for _, res := range f.projectResources {
		nResources += len(res)
	}
	r.add("initiative_resources", nResources)
	r.add("tickets", len(f.issues))
	nComments, nReactions, nNoteMarks, nSubs, nPRs := 0, 0, 0, 0, 0
	for _, i := range f.issues {
		nReactions += len(i.Reactions)
		nSubs += len(f.subscribers[i.ID])
		nPRs += len(f.prLinks[i.ID])
		cs := f.comments[i.ID]
		nComments += len(cs)
		for _, c := range cs {
			nNoteMarks += len(c.Reactions)
		}
	}
	r.add("ticket_notes", nComments)
	r.add("ticket_marks", nReactions)
	r.add("note_marks", nNoteMarks)
	r.add("ticket_subscribers", nSubs)
	r.add("ticket_pr_links", nPRs)
	r.add("sentinels", len(f.autopilots))
	nTriggers := 0
	for _, t := range f.autopilotTriggers {
		nTriggers += len(t)
	}
	r.add("sentinel_triggers", nTriggers)
	r.add("convos", len(f.chatSessions))
	nMsgs := 0
	for _, m := range f.chatMessages {
		nMsgs += len(m)
	}
	r.add("convo_messages", nMsgs)
	r.add("space_mcp_servers", len(f.mcpServers))
	if f.config != nil {
		r.add("space_config", 1)
	}
}

// recordSkips фиксирует в отчёте категории данных, которые сознательно не
// переносятся — см. docs/51-data-model.md «Что не переносится через API, и
// почему» и server2/docs/decisions.md «Пробелы спецификации».
func recordSkips(r *Report, f *fetched) {
	r.skip("login_sessions", 0, "контракт не отдаёт хэши секретов сессий/MFA ни одному клиенту; участники входят заново")
	r.skip("access_keys", 0, "PAT виден только в момент выпуска; участники выпускают новые токены после переноса")
	r.skip("webhook_secrets", 0, "signing_secret/webhook_token не отдаются контрактом на чтение; секреты перевыпускаются после переноса (rotate-webhook-token/signing-secret)")
	r.skip("sealed_configs", 0, "runtime_config.gateway.token, mcp_config (при mcp_config_redacted), cfg_llm_api_key и credential-значения MCP замаскированы контрактом или требуют ключа шифрования server2, недоступного импортёру; переносятся только незасекреченные поля")
	r.skip("vcs_github_slack_composio_oauth", 0, "OAuth-токены сторонних интеграций привязаны к конкретному деплою (client_id/callback URL) и физически не валидны в новом; интеграции переустанавливаются заново")
	r.skip("wallet_billing", 0, "биллинг — отдельный процесс переноса вне T-025 (см. docs/51-data-model.md)")
	r.skip("platform_admin_audit", 0, "аудит и права уровня деплоя не принадлежат переносимому воркспейсу")
	nAttachments := 0
	r.skip("assets_attachments", nAttachments, "перенос байтов вложений требует доступа к объектному хранилищу целевого деплоя, которое не описано в контракте и недоступно импортёру; метаданные/файлы не переносятся в этой версии (см. decisions.md)")
	r.skip("inbox_history", 0, "InboxItem не имеет bulk GET/POST в контракте; уведомления перегенерируются штатной логикой сервера как побочный эффект переноса issues/comments")
	nOtherSessionsChats := 0
	r.skip("chat_sessions_of_other_members", nOtherSessionsChats, "GET /api/chat/sessions контракта возвращает только сессии, СОЗДАННЫЕ вызывающим PAT — bulk-листинга по всему воркспейсу для owner/admin контракт не выставляет (см. decisions.md); переносятся только чаты владельца токена импорта")
	r.skip("dispatch_jobs_raw", 0, "исполнительный журнал очереди задач не выставлен bulk-эндпоинтом контракта; переносится только то, что видно через ресурсы задачи (комментарии/статусы)")
}
