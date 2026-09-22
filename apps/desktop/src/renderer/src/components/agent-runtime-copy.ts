import type { useT } from '@goosar/views/i18n';
import type { DaemonState } from '../../../shared/daemon-types';
import type {
  AgentConfigField,
  AgentRuntimeStatus,
  McpClientState,
} from '../../../shared/agent-runtime-types';

type SettingsT = ReturnType<typeof useT<'settings'>>['t'];

export function agentConfigFieldLabel(t: SettingsT, field: AgentConfigField): string {
  switch (field) {
    case 'llm.api_base':
      return t(($) => $.desktop.agent_runtime.field_label.api_base);
    case 'llm.model':
      return t(($) => $.desktop.agent_runtime.field_label.model);
    case 'llm.api_key':
      return t(($) => $.desktop.agent_runtime.field_label.api_key);
  }
}

export function agentConfigFieldHint(t: SettingsT, field: AgentConfigField): string {
  switch (field) {
    case 'llm.api_base':
      return t(($) => $.desktop.agent_runtime.field_hint.api_base);
    case 'llm.model':
      return t(($) => $.desktop.agent_runtime.field_hint.model);
    case 'llm.api_key':
      return t(($) => $.desktop.agent_runtime.field_hint.api_key);
  }
}

export function listFieldLabels(t: SettingsT, fields: AgentConfigField[]): string {
  const names = fields.map((field) => agentConfigFieldLabel(t, field));
  if (names.length <= 1) return names[0] ?? '';
  const conjunction = t(($) => $.desktop.agent_runtime.list_and);
  return `${names.slice(0, -1).join(', ')}${conjunction}${names[names.length - 1]}`;
}

export function agentRuntimeLabel(t: SettingsT, state: AgentRuntimeStatus['state']): string {
  switch (state) {
    case 'checking':
      return t(($) => $.desktop.agent_runtime.label.checking);
    case 'unsupported':
      return t(($) => $.desktop.agent_runtime.label.unsupported);
    case 'not_installed':
      return t(($) => $.desktop.agent_runtime.label.not_installed);
    case 'external':
      return t(($) => $.desktop.agent_runtime.label.external);
    case 'needs_config':
      return t(($) => $.desktop.agent_runtime.label.needs_config);
    case 'ready':
      return t(($) => $.desktop.agent_runtime.label.ready);
  }
}

export function daemonStateLabel(t: SettingsT, state: DaemonState): string {
  switch (state) {
    case 'running':
      return t(($) => $.desktop.daemon.state.running);
    case 'stopped':
      return t(($) => $.desktop.daemon.state.stopped);
    case 'starting':
      return t(($) => $.desktop.daemon.state.starting);
    case 'stopping':
      return t(($) => $.desktop.daemon.state.stopping);
    case 'installing_cli':
      return t(($) => $.desktop.daemon.state.installing_cli);
    case 'cli_not_found':
      return t(($) => $.desktop.daemon.state.cli_not_found);
    case 'auth_expired':
      return t(($) => $.desktop.daemon.state.auth_expired);
  }
}

export function mcpClientSentence(t: SettingsT, mcp: McpClientState | undefined): string {
  if (!mcp) return '';
  switch (mcp.state) {
    case 'ok':
      return '';
    case 'absent':
      return ` ${t(($) => $.desktop.agent_runtime.mcp_absent)}`;
    case 'incompatible':
      return ` ${t(($) => $.desktop.agent_runtime.mcp_incompatible, { reason: mcp.reason })}`;
    case 'unknown':
      return ` ${t(($) => $.desktop.agent_runtime.mcp_unknown, { detail: mcp.detail })}`;
    default: {
      const exhaustive: never = mcp;
      return String(exhaustive);
    }
  }
}

function formatMissingFields(t: SettingsT, fields: AgentConfigField[]): string {
  const names = fields.map((field) => agentConfigFieldLabel(t, field).toLocaleLowerCase());
  if (names.length === 0) return t(($) => $.desktop.agent_runtime.missing_none);
  if (names.length === 1) return names[0]!;
  const conjunction = t(($) => $.desktop.agent_runtime.list_and);
  return `${names.slice(0, -1).join(', ')}${conjunction}${names[names.length - 1]}`;
}

function agentRuntimeStateSentence(t: SettingsT, status: AgentRuntimeStatus): string {
  switch (status.state) {
    case 'checking':
      return t(($) => $.desktop.agent_runtime.desc_checking);
    case 'unsupported':
      return status.detail;
    case 'not_installed':
      return t(($) => $.desktop.agent_runtime.desc_not_installed, {
        detail: status.detail,
      });
    case 'external':
      return t(($) => $.desktop.agent_runtime.desc_external, {
        path: status.path,
      });
    case 'needs_config':
      return status.configPath === null
        ? t(($) => $.desktop.agent_runtime.desc_needs_config_no_file, {
            version: status.version,
          })
        : t(($) => $.desktop.agent_runtime.desc_needs_config, {
            version: status.version,
            path: status.configPath,
            fields: formatMissingFields(t, status.missing),
          });
    case 'ready':
      return t(($) => $.desktop.agent_runtime.desc_ready, {
        version: status.version,
        path: status.configPath,
      });
    default: {
      const exhaustive: never = status;
      return String(exhaustive);
    }
  }
}

export function agentRuntimeDescription(t: SettingsT, status: AgentRuntimeStatus): string {
  const base =
    agentRuntimeStateSentence(t, status) +
    (status.state === 'ready' || status.state === 'needs_config'
      ? mcpClientSentence(t, status.mcpClient)
      : '');
  if ((status.state === 'ready' || status.state === 'needs_config') && status.userBinConflict) {
    return `${base} ${t(($) => $.desktop.agent_runtime.user_bin_conflict, { path: status.userBinConflict })}`;
  }
  return base;
}
