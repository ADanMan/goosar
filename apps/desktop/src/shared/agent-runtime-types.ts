// Ключи hermes config set, которые десктоп умеет заполнять, в точной записи
// dotted-ключей runtime, чтобы значение можно было передать обратно как есть.
export type AgentConfigField = 'llm.api_base' | 'llm.model' | 'llm.api_key';

export const AGENT_CONFIG_FIELDS: readonly AgentConfigField[] = [
  'llm.api_base',
  'llm.model',
  'llm.api_key',
];

export type AgentBashFullField = 'security.bash_full';
export const AGENT_BASH_FULL_FIELD: AgentBashFullField = 'security.bash_full';

export type AgentConfigPatch = Partial<Record<AgentConfigField, string>> &
  Partial<Record<AgentBashFullField, boolean>>;

export type AgentConfigErrorKind =
  | 'runtime_unavailable'
  /** hermes found no config file to write into (exit 1). */
  | 'config_unavailable'
  /** Bad argument combination — a bug on our side (exit 2). */
  | 'bad_usage'
  /** The key is not part of hermes's schema (exit 3). */
  | 'bad_key'
  /** The value failed validation; the file was not touched (exit 4). */
  | 'invalid_value'
  /** The file could not be written (exit 5). */
  | 'write_failed'
  /** The runtime answered in a way we cannot interpret. */
  | 'unknown';

export type AgentConfigSaveResult =
  | {
      ok: true;
      path: string;
      shadowed: AgentConfigField[];
    }
  | {
      ok: false;
      field: AgentConfigField | null;
      kind: AgentConfigErrorKind;
      message: string;
    };

export type McpClientState =
  | { state: 'unknown'; detail: string }
  /** The SDK's models still carry the fields the agent reads. */
  | { state: 'ok'; version?: string }
  /** No `mcp` SDK: by design a no-op, so no integrations at all. */
  | { state: 'absent'; detail?: string }
  /** Present and unusable: every server would start and expose no tools. */
  | { state: 'incompatible'; version?: string; reason: string };

export type AgentRuntimeStatus =
  | { state: 'checking' }
  | { state: 'unsupported'; detail: string }
  | { state: 'not_installed'; detail: string }
  | { state: 'external'; path: string }
  | {
      state: 'needs_config';
      version: string;
      binPath: string;
      configPath: string | null;
      missing: AgentConfigField[];
      userBinConflict?: string;
      mcpClient?: McpClientState;
    }
  | {
      state: 'ready';
      version: string;
      binPath: string;
      configPath: string;
      userBinConflict?: string;
      mcpClient?: McpClientState;
    };

export const AGENT_RUNTIME_COLORS: Record<AgentRuntimeStatus['state'], string> = {
  checking: 'bg-sky-500 animate-pulse',
  unsupported: 'bg-muted-foreground/40',
  not_installed: 'bg-muted-foreground/40',
  external: 'bg-sky-500',
  needs_config: 'bg-amber-500',
  ready: 'bg-emerald-500',
};

export type AgentRunnerChoice = 'none' | 'hermes';

export const AGENT_RUNNER_CHOICES: readonly AgentRunnerChoice[] = ['none', 'hermes'];

export const DEFAULT_AGENT_RUNNER: AgentRunnerChoice = 'none';

export interface AgentRunnerState {
  choice: AgentRunnerChoice;
  bundledAvailable: boolean;
  bundledVersion: string | null;
}

export interface AgentRunnerSetResult {
  success: boolean;
  error?: string;
  choice: AgentRunnerChoice;
  runtime: AgentRuntimeStatus;
  daemonRestart?: 'restarted' | 'not_running' | 'foreign' | 'failed';
}
