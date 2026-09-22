'use client';

import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import type { McpCredentialField, WorkspaceMcpServer } from '@goosar/core/api/workspace-mcp';
import { Button } from '@goosar/ui/components/ui/button';
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

export function McpCredentialsDialog({
  open,
  server,
  saving,
  onOpenChange,
  onSave,
}: {
  open: boolean;
  server: WorkspaceMcpServer | null;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (values: Record<string, string>) => Promise<void>;
}) {
  const { t } = useT('settings');
  const [values, setValues] = useState<Record<string, string>>({});

  useEffect(() => {
    if (open) setValues({});
  }, [open, server?.id]);

  if (server === null) return null;
  const fields = server.credential_schema ?? [];
  const provided = new Set(server.provided_credentials ?? []);
  const filled = Object.entries(values).filter(([, value]) => value.trim() !== '');
  const stillMissing = fields.filter(
    (field) =>
      field.required && !provided.has(field.key) && (values[field.key] ?? '').trim() === '',
  );

  return (
    <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.mcp.credentials_title, { name: server.name })}</DialogTitle>
          <DialogDescription>{t(($) => $.mcp.credentials_description)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {fields.map((field) => (
            <CredentialInput
              key={field.key}
              field={field}
              alreadyProvided={provided.has(field.key)}
              value={values[field.key] ?? ''}
              onChange={(next) => setValues((current) => ({ ...current, [field.key]: next }))}
            />
          ))}
        </div>

        {/*
          Said before the save, not after it: sending a partial set leaves the
          integration exactly as broken as it was, and the person deserves to
          know that while they can still do something about it.
        */}
        {stillMissing.length > 0 ? (
          <p className="text-xs text-amber-600 dark:text-amber-500">
            {t(($) => $.mcp.credentials_still_missing, {
              fields: stillMissing.map((field) => field.key).join(', '),
            })}
          </p>
        ) : null}

        <DialogFooter>
          <Button variant="outline" disabled={saving} onClick={() => onOpenChange(false)}>
            {t(($) => $.mcp.cancel)}
          </Button>
          <Button
            disabled={saving || filled.length === 0}
            onClick={() => {
              void onSave(Object.fromEntries(filled));
            }}
          >
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t(($) => $.mcp.credentials_save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CredentialInput({
  field,
  alreadyProvided,
  value,
  onChange,
}: {
  field: McpCredentialField;
  alreadyProvided: boolean;
  value: string;
  onChange: (next: string) => void;
}) {
  const { t } = useT('settings');
  const inputId = `mcp-credential-${field.key}`;
  return (
    <div className="space-y-1.5">
      <Label htmlFor={inputId}>
        {field.label && field.label !== '' ? field.label : field.key}
        {field.required === true ? <span className="ml-1 text-muted-foreground">*</span> : null}
      </Label>
      <Input
        id={inputId}
        type="password"
        autoComplete="off"
        spellCheck={false}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={alreadyProvided ? t(($) => $.mcp.credentials_replace_placeholder) : field.key}
      />
      {/* The admin's own words. This is the whole difference between "give me
          a token" and a request somebody can act on. */}
      {field.hint && field.hint !== '' ? (
        <p className="text-xs leading-5 text-muted-foreground">{field.hint}</p>
      ) : null}
    </div>
  );
}
