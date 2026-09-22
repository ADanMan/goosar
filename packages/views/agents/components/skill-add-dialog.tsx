'use client';

import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import type { Agent, SkillSummary } from '@goosar/core/types';
import { api } from '@goosar/core/api';
import { useWorkspaceId } from '@goosar/core/hooks';
import { skillListOptions, workspaceKeys } from '@goosar/core/workspace/queries';
import { Button } from '@goosar/ui/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { useT } from '../../i18n';
import { SkillPickerList } from './skill-picker-list';

export function SkillAddDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: Agent;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const { t } = useT('agents');
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: workspaceSkills = [], isLoading } = useQuery(skillListOptions(wsId));
  const [saving, setSaving] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  const attachedIds = useMemo(() => new Set(agent.skills.map((s) => s.id)), [agent.skills]);
  const availableSkills = useMemo(
    () => workspaceSkills.filter((s) => !attachedIds.has(s.id)),
    [workspaceSkills, attachedIds],
  );

  const handleOpenChange = (v: boolean) => {
    if (!v) setSelectedIds(new Set());
    onOpenChange(v);
  };

  const handleToggle = (skill: SkillSummary) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(skill.id)) next.delete(skill.id);
      else next.add(skill.id);
      return next;
    });
  };

  const handleConfirm = async () => {
    if (selectedIds.size === 0) return;
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), ...selectedIds];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      handleOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.skills.add_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  const count = selectedIds.size;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">
            {t(($) => $.tab_body.skills.add_dialog_title)}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.tab_body.skills.add_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <SkillPickerList
          skills={availableSkills}
          selectedIds={selectedIds}
          onToggle={handleToggle}
          loading={isLoading}
          emptyMessage={
            workspaceSkills.length === 0
              ? t(($) => $.tab_body.skills.add_dialog_empty)
              : t(($) => $.tab_body.skills.add_dialog_empty_partial)
          }
        />

        <DialogFooter>
          <Button variant="ghost" onClick={() => handleOpenChange(false)}>
            {t(($) => $.tab_body.skills.add_dialog_cancel)}
          </Button>
          <Button onClick={handleConfirm} disabled={count === 0 || saving}>
            {saving
              ? t(($) => $.tab_body.skills.add_dialog_saving)
              : count > 0
                ? t(($) => $.tab_body.skills.add_dialog_confirm, { count })
                : t(($) => $.tab_body.skills.add_dialog_confirm_default)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
