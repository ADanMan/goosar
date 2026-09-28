package daemon

import (
	"context"
	"encoding/json"
)

// noteContent — текст+автор комментария-триггера (ticket_notes).
func (d *Deps) noteContent(ctx context.Context, noteID string) (content string, authorType, authorID *string, err error) {
	err = d.DB.Pool.QueryRow(ctx, `SELECT tn_body, tn_author_type, tn_author_id FROM ticket_notes WHERE id = $1`, noteID).
		Scan(&content, &authorType, &authorID)
	return content, authorType, authorID, err
}

// coalescedComments — TaskCoalescedComment[] для dj_coalesced_note_ids, в
// том порядке, в котором id перечислены в очереди (не по времени создания:
// массив уже несёт порядок объединения).
func (d *Deps) coalescedComments(ctx context.Context, ids []string) []map[string]any {
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT id, tn_parent_note_id, tn_author_type, tn_author_id, tn_body, created_at
		FROM ticket_notes WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil
	}
	defer rows.Close()
	byID := map[string]map[string]any{}
	for rows.Next() {
		var id string
		var threadID, authorType, authorID *string
		var body string
		var createdAt any
		if err := rows.Scan(&id, &threadID, &authorType, &authorID, &body, &createdAt); err != nil {
			continue
		}
		name, _ := d.actorName(ctx, authorType, authorID)
		item := map[string]any{"id": id, "content": body, "created_at": createdAt}
		if threadID != nil {
			item["thread_id"] = *threadID
		}
		if authorType != nil {
			item["author_type"] = *authorType
		}
		item["author_name"] = name
		byID[id] = item
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

// projectResources — TaskProjectResource[] (initiative_resources проекта).
func (d *Deps) projectResources(ctx context.Context, initiativeID *string) ([]map[string]any, error) {
	if initiativeID == nil {
		return nil, nil
	}
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT id, ir_resource_type, ir_resource_ref, ir_label FROM initiative_resources
		WHERE initiative_id = $1 ORDER BY ir_position`, *initiativeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, kind string
		var ref json.RawMessage
		var label *string
		if err := rows.Scan(&id, &kind, &ref, &label); err != nil {
			return nil, err
		}
		item := map[string]any{"id": id, "resource_type": kind, "resource_ref": rawOrNil(ref)}
		if label != nil {
			item["label"] = *label
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// agentSkills — AgentSkillBundle[] полным содержимым (skills, не skill_refs
// — упрощение: этот сервер не разбирает X-Client-Capabilities: skill-bundles-v1,
// см. decisions.md, поэтому всегда шлёт полное содержимое, как для клиента
// без этой capability).
func (d *Deps) agentSkills(ctx context.Context, operativeID string) ([]map[string]any, error) {
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT c.id, c.cap_title, c.cap_summary, c.cap_body_md
		FROM operative_capabilities oc JOIN capabilities c ON c.id = oc.capability_id
		WHERE oc.operative_id = $1 AND oc.opcap_enabled`, operativeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, title string
		var summary *string
		var body string
		if err := rows.Scan(&id, &title, &summary, &body); err != nil {
			return nil, err
		}
		item := map[string]any{"id": id, "source": "workspace", "name": title, "content": body, "size_bytes": len(body)}
		if summary != nil {
			item["description"] = *summary
		}
		files, ferr := d.capabilityFiles(ctx, id)
		if ferr == nil && len(files) > 0 {
			item["files"] = files
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *Deps) capabilityFiles(ctx context.Context, capabilityID string) ([]map[string]any, error) {
	rows, err := d.DB.Pool.Query(ctx, `SELECT capf_path, capf_body FROM capability_files WHERE capability_id = $1`, capabilityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var path, body string
		if err := rows.Scan(&path, &body); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"path": path, "content": body, "size_bytes": len(body)})
	}
	return out, rows.Err()
}

// disabledRuntimeSkills — DisabledRuntimeSkill[] для агента на этом рантайме.
func (d *Deps) disabledRuntimeSkills(ctx context.Context, operativeID, executorID string) ([]map[string]any, error) {
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT opdis_provider, opdis_root, opdis_key, opdis_title, opdis_plugin
		FROM operative_disabled_local_skills WHERE operative_id = $1 AND executor_id = $2`, operativeID, executorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var provider, root, key string
		var title, plugin *string
		if err := rows.Scan(&provider, &root, &key, &title, &plugin); err != nil {
			return nil, err
		}
		item := map[string]any{"runtime_id": executorID, "provider": provider, "root": root, "key": key}
		if title != nil {
			item["name"] = *title
		}
		if plugin != nil {
			item["plugin"] = *plugin
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
