'use client';

import { useState } from 'react';
import { Plus, X } from 'lucide-react';
import { toast } from 'sonner';
import { useQuery } from '@tanstack/react-query';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@goosar/ui/components/ui/alert-dialog';
import { Badge } from '@goosar/ui/components/ui/badge';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Switch } from '@goosar/ui/components/ui/switch';
import {
  useUpdateWorkspaceConfig,
  workspaceConfigOptions,
} from '@goosar/core/workspace/admin-config';
import type { McpConfigEntry, McpConfigEntryPatch } from '@goosar/core/api/workspace-admin';
import { useT } from '../../i18n';
import { SettingsCard, SettingsSection } from './settings-layout';

type PendingWrite = {
  op: 'disable' | 'remove' | 'env-remove' | 'env-set';
  name: string;
  env?: string;
  patch: McpConfigEntryPatch | null;
  onSuccess?: () => void;
};

export function AdminMcpDefaultsSection({ wsId }: { wsId: string }) {
  const { t } = useT('settings');
  const { data: config } = useQuery(workspaceConfigOptions(wsId));
  const updateConfig = useUpdateWorkspaceConfig(wsId);

  const [newServerName, setNewServerName] = useState('');
  const [pending, setPending] = useState<PendingWrite | null>(null);

  const entries = config?.mcp_defaults ?? {};
  const serverNames = Object.keys(entries).sort();

  const patchEntry = async (name: string, patch: McpConfigEntryPatch | null) => {
    try {
      await updateConfig.mutateAsync({ mcp_defaults: { [name]: patch } });
      toast.success(t(($) => $.admin.mcp.toast_saved));
      return true;
    } catch {
      toast.error(t(($) => $.admin.mcp.toast_save_failed));
      return false;
    }
  };

  const applyPending = async () => {
    if (!pending) return;
    const ok = await patchEntry(pending.name, pending.patch);
    if (!ok) return;
    pending.onSuccess?.();
    setPending(null);
  };

  const toggleServer = (name: string) => {
    const entry = entries[name];
    if (!entry) return;
    if (entry.enabled === false) {
      void patchEntry(name, { enabled: true });
      return;
    }
    setPending({ op: 'disable', name, patch: { enabled: false } });
  };

  const removeServer = (name: string) => {
    setPending({ op: 'remove', name, patch: null });
  };

  const addServer = () => {
    const name = newServerName.trim();
    if (!name || entries[name]) return;
    void patchEntry(name, { enabled: true }).then((ok) => {
      if (ok) setNewServerName('');
    });
  };

  const removeEnvVar = (serverName: string, envName: string) => {
    setPending({
      op: 'env-remove',
      name: serverName,
      env: envName,
      patch: { env: { [envName]: null } },
    });
  };

  const setEnvVar = (serverName: string, envName: string, value: string, onSuccess: () => void) => {
    setPending({
      op: 'env-set',
      name: serverName,
      env: envName,
      patch: { env: { [envName]: value } },
      onSuccess,
    });
  };

  const confirmCopy = (write: PendingWrite) => {
    const name = write.name;
    const env = write.env ?? '';
    switch (write.op) {
      case 'disable':
        return {
          title: t(($) => $.admin.mcp.confirm_disable_title, { name }),
          description: t(($) => $.admin.mcp.confirm_disable_description, {
            name,
          }),
        };
      case 'remove':
        return {
          title: t(($) => $.admin.mcp.confirm_remove_title, { name }),
          description: t(($) => $.admin.mcp.confirm_remove_description, {
            name,
          }),
        };
      case 'env-remove':
        return {
          title: t(($) => $.admin.mcp.confirm_env_remove_title, { name, env }),
          description: t(($) => $.admin.mcp.confirm_env_remove_description, {
            name,
            env,
          }),
        };
      case 'env-set':
        return {
          title: t(($) => $.admin.mcp.confirm_env_set_title, { name, env }),
          description: t(($) => $.admin.mcp.confirm_env_set_description, {
            name,
            env,
          }),
        };
    }
  };

  return (
    <SettingsSection
      title={t(($) => $.admin.mcp.title)}
      description={t(($) => $.admin.mcp.description)}
    >
      <SettingsCard>
        {serverNames.length === 0 && (
          <div className="px-4 py-3 text-sm text-muted-foreground">
            {t(($) => $.admin.mcp.empty)}
          </div>
        )}
        {serverNames.map((name) => (
          <McpServerRow
            key={name}
            name={name}
            entry={entries[name]!}
            busy={updateConfig.isPending}
            onToggle={() => toggleServer(name)}
            onRemove={() => removeServer(name)}
            onRemoveEnvVar={(envName) => removeEnvVar(name, envName)}
            onSetEnvVar={(envName, value, onSuccess) => setEnvVar(name, envName, value, onSuccess)}
          />
        ))}
        <div className="flex items-center gap-2 px-4 py-3">
          <Input
            className="sm:w-64"
            value={newServerName}
            onChange={(e) => setNewServerName(e.target.value)}
            placeholder={t(($) => $.admin.mcp.add_server_placeholder)}
            aria-label={t(($) => $.admin.mcp.add_server_placeholder)}
            autoComplete="off"
          />
          <Button
            variant="outline"
            size="sm"
            onClick={addServer}
            disabled={updateConfig.isPending || newServerName.trim() === ''}
          >
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.admin.mcp.add_server)}
          </Button>
        </div>
      </SettingsCard>

      {/* §3 L2: one confirm in front of every subtractive/secret write to the
          workspace MCP defaults (issue #244). */}
      <AlertDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) setPending(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            {/* Server and variable names are unbounded, so the heading wraps
                instead of escaping the popup. */}
            <AlertDialogTitle className="break-words">
              {pending ? confirmCopy(pending).title : ''}
            </AlertDialogTitle>
            <AlertDialogDescription className="break-words">
              {pending ? confirmCopy(pending).description : ''}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.admin.mcp.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={applyPending} disabled={updateConfig.isPending}>
              {t(($) => $.admin.mcp.confirm_apply)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}

function McpServerRow({
  name,
  entry,
  busy,
  onToggle,
  onRemove,
  onRemoveEnvVar,
  onSetEnvVar,
}: {
  name: string;
  entry: McpConfigEntry;
  busy: boolean;
  onToggle: () => void;
  onRemove: () => void;
  onRemoveEnvVar: (envName: string) => void;
  onSetEnvVar: (envName: string, value: string, onSuccess: () => void) => void;
}) {
  const { t } = useT('settings');
  const [newEnvName, setNewEnvName] = useState('');
  const [newEnvValue, setNewEnvValue] = useState('');

  const envNames = Object.keys(entry.env ?? {}).sort();

  const addEnvVar = () => {
    const envName = newEnvName.trim();
    if (!envName) return;
    onSetEnvVar(envName, newEnvValue, () => {
      setNewEnvName('');
      setNewEnvValue('');
    });
  };

  return (
    <div className="space-y-3 px-4 py-3">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <span className="text-sm font-medium">{name}</span>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t(($) => $.admin.mcp.remove_server_aria, { name })}
          onClick={onRemove}
          disabled={busy}
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
        <Switch
          checked={entry.enabled !== false}
          onCheckedChange={onToggle}
          aria-label={
            entry.enabled !== false
              ? t(($) => $.admin.mcp.toggle_off_aria, { name })
              : t(($) => $.admin.mcp.toggle_aria, { name })
          }
          disabled={busy}
        />
      </div>
      <div className="space-y-2 pl-1">
        {envNames.map((envName) => (
          <div key={envName} className="flex items-center gap-2 text-xs">
            <code className="font-mono">{envName}</code>
            {/* The stored value stays server-side only — never in the DOM. */}
            <span className="text-muted-foreground">{t(($) => $.admin.mcp.env_value_hidden)}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.admin.mcp.env_remove_aria, {
                env: envName,
                name,
              })}
              onClick={() => onRemoveEnvVar(envName)}
              disabled={busy}
            >
              <X className="h-3 w-3 text-muted-foreground" />
            </Button>
          </div>
        ))}
        <div className="flex items-center gap-2">
          <Input
            className="h-7 sm:w-40"
            value={newEnvName}
            onChange={(e) => setNewEnvName(e.target.value)}
            placeholder={t(($) => $.admin.mcp.env_name_placeholder)}
            aria-label={t(($) => $.admin.mcp.env_name_placeholder)}
            autoComplete="off"
          />
          <Input
            className="h-7 sm:w-48"
            type="password"
            value={newEnvValue}
            onChange={(e) => setNewEnvValue(e.target.value)}
            placeholder={t(($) => $.admin.mcp.env_value_placeholder)}
            aria-label={t(($) => $.admin.mcp.env_value_aria, { name })}
            autoComplete="off"
          />
          <Button
            variant="ghost"
            size="sm"
            onClick={addEnvVar}
            disabled={busy || newEnvName.trim() === ''}
          >
            {t(($) => $.admin.mcp.env_add)}
          </Button>
        </div>
      </div>
      {entry.enabled === false && (
        <Badge variant="outline">{t(($) => $.admin.pins.disabled_badge)}</Badge>
      )}
    </div>
  );
}
