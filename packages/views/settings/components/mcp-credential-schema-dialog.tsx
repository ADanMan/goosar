'use client';

import { useEffect, useState } from 'react';
import { Loader2, Plus, Trash2 } from 'lucide-react';
import type { McpCredentialField } from '@goosar/core/api/workspace-mcp';
import { Button } from '@goosar/ui/components/ui/button';
import { Checkbox } from '@goosar/ui/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { useT } from '../../i18n';

export const ATLASSIAN_PAT_PRESET_FIELDS: McpCredentialField[] = [
  {
    key: 'JIRA_PERSONAL_TOKEN',
    label: 'Jira personal access token',
    hint: 'Server/Data Center Personal Access Token from the Jira profile settings.',
    required: true,
  },
  {
    key: 'CONFLUENCE_PERSONAL_TOKEN',
    label: 'Confluence personal access token',
    hint: 'Server/Data Center Personal Access Token from the Confluence profile settings.',
    required: true,
  },
];

export function McpCredentialSchemaDialog({
  open,
  server,
  saving,
  onOpenChange,
  onSave,
}: {
  open: boolean;
  server: {
    id: string;
    name: string;
    credential_schema?: McpCredentialField[];
  } | null;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (schema: McpCredentialField[]) => Promise<void>;
}) {
  const { t } = useT('settings');
  const [fields, setFields] = useState<McpCredentialField[]>([]);

  useEffect(() => {
    if (open) setFields(server?.credential_schema ?? []);
  }, [open, server?.id, server?.credential_schema]);

  if (server === null) return null;

  const update = (index: number, patch: Partial<McpCredentialField>) => {
    setFields((current) =>
      current.map((field, i) => (i === index ? { ...field, ...patch } : field)),
    );
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.mcp.schema_title, { name: server.name })}</DialogTitle>
          <DialogDescription>{t(($) => $.mcp.schema_description)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {fields.map((field, index) => (
            <div key={index} className="space-y-2 rounded-md border border-border p-3">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1 space-y-1.5">
                  <Label htmlFor={`mcp-schema-key-${index}`}>
                    {t(($) => $.mcp.schema_key_label)}
                  </Label>
                  <Input
                    id={`mcp-schema-key-${index}`}
                    autoComplete="off"
                    spellCheck={false}
                    value={field.key}
                    placeholder="JIRA_TOKEN"
                    onChange={(event) => update(index, { key: event.target.value })}
                  />
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="mt-6"
                  aria-label={t(($) => $.mcp.schema_remove_field)}
                  onClick={() => setFields((current) => current.filter((_, i) => i !== index))}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={`mcp-schema-label-${index}`}>
                  {t(($) => $.mcp.schema_label_label)}
                </Label>
                <Input
                  id={`mcp-schema-label-${index}`}
                  value={field.label ?? ''}
                  onChange={(event) => update(index, { label: event.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor={`mcp-schema-hint-${index}`}>
                  {t(($) => $.mcp.schema_hint_label)}
                </Label>
                <Input
                  id={`mcp-schema-hint-${index}`}
                  value={field.hint ?? ''}
                  onChange={(event) => update(index, { hint: event.target.value })}
                />
                {/* The one instruction on this screen worth reading twice: the
                    text typed here is the whole of what a blocked user is
                    given to act on. */}
                <p className="text-xs text-muted-foreground">{t(($) => $.mcp.schema_hint_help)}</p>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={field.required === true}
                  onCheckedChange={(next) => update(index, { required: next === true })}
                />
                {t(($) => $.mcp.schema_required_label)}
              </label>
            </div>
          ))}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                setFields((current) => [
                  ...current,
                  { key: '', label: '', hint: '', required: true },
                ])
              }
            >
              <Plus className="h-4 w-4" />
              {t(($) => $.mcp.schema_add_field)}
            </Button>
            {/* T-11 (#639): the same PAT-only field pair the onboarding
                Atlassian preset settled on in T-09 — no username/api_token,
                Server/Data Center personal access tokens only. */}
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                setFields((current) => [
                  ...current,
                  ...ATLASSIAN_PAT_PRESET_FIELDS.filter(
                    (preset) => !current.some((field) => field.key === preset.key),
                  ),
                ])
              }
            >
              <Plus className="h-4 w-4" />
              {t(($) => $.mcp.schema_preset_atlassian_pat)}
            </Button>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" disabled={saving} onClick={() => onOpenChange(false)}>
            {t(($) => $.mcp.cancel)}
          </Button>
          <Button
            disabled={saving}
            onClick={() => {
              void onSave(
                fields
                  .map((field) => ({ ...field, key: field.key.trim() }))
                  .filter((field) => field.key !== ''),
              );
            }}
          >
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t(($) => $.mcp.schema_save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
