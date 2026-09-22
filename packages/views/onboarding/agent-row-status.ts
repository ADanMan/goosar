// Статус строки «Агент» на шаге подготовки воркспейса в онбординге.

export type AgentRuntimeRowState =
  | 'checking'
  | 'installing'
  | 'ready'
  | 'unsupported'
  | 'not_installed'
  | 'external'
  | 'needs_config';

export interface AgentRuntimeStatus {
  state: AgentRuntimeRowState;
  version?: string;
  detail?: string;
  bashProbe?: { ok: boolean; reason?: string } | null;
}
